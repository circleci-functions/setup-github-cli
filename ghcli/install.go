package ghcli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/circleci-functions/toolkit/archive"
	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/download"
)

// DefaultBinDir is where gh is symlinked by default, so it is on PATH even for
// steps that do not source $BASH_ENV.
const DefaultBinDir = "/usr/local/bin"

// latestReleaseURL redirects to the newest gh release's tag page. It is a var so
// tests can point resolution at a local server.
var latestReleaseURL = "https://github.com/cli/cli/releases/latest"

// ResolveVersion turns a version spec into a concrete semver tag ("v2.37.0").
// "latest" (or "") is resolved from the releases page, retrying up to maxRetries
// times; any other spec is normalised to a leading-"v" tag and validated.
func ResolveVersion(ctx context.Context, spec string, maxRetries int) (string, error) {
	if spec != "" && spec != "latest" {
		v := spec
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		if !semver.IsValid(v) {
			return "", fmt.Errorf("invalid version %q", spec)
		}
		return v, nil
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tag, err := latestTag(ctx)
		if err == nil {
			return tag, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("resolve latest gh release after %d retries: %w", maxRetries, lastErr)
}

// latestTag follows the releases/latest redirect and reads the tag from the
// final URL (.../releases/tag/v2.37.0).
func latestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}

	tag := path.Base(resp.Request.URL.Path)
	if !semver.IsValid(tag) {
		return "", fmt.Errorf("could not parse a version from %q", resp.Request.URL.Path)
	}
	return tag, nil
}

// InstallOptions configures EnsureInstalled.
type InstallOptions struct {
	InstallDir string
	// BinDir is where gh is symlinked; DefaultBinDir when empty.
	BinDir string
}

// EnsureInstalled installs gh at version (a semver tag) for the current platform
// under opts.InstallDir, symlinks it into opts.BinDir, prepends its bin
// directory to $BASH_ENV PATH, and returns the path to the gh binary. A version
// already present under InstallDir is reused rather than downloaded again.
func EnsureInstalled(ctx context.Context, version string, opts InstallOptions) (string, error) {
	plat, err := DetectPlatform()
	if err != nil {
		return "", err
	}

	binDir := opts.BinDir
	if binDir == "" {
		binDir = DefaultBinDir
	}

	ghBin := filepath.Join(opts.InstallDir, version, "bin", "gh")
	if installedVersion(ghBin) == version {
		fmt.Fprintf(os.Stderr, "setup-github-cli: gh %s already installed at %s, skipping download\n", version, ghBin)
	} else if err := install(ctx, plat, version, opts.InstallDir); err != nil {
		return "", err
	}

	symlinkGH(ghBin, binDir)

	if err := bashenv.AddToPath(filepath.Dir(ghBin)); err != nil {
		return "", fmt.Errorf("add gh to PATH: %w", err)
	}
	return ghBin, nil
}

// installedVersion returns the semver tag reported by the gh binary at ghBin, or
// "" if it is absent or unparseable. gh prints "gh version 2.37.0 (2023-...)".
func installedVersion(ghBin string) string {
	out, err := exec.Command(ghBin, "--version").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return ""
	}
	v := "v" + fields[2]
	if !semver.IsValid(v) {
		return ""
	}
	return v
}

func install(ctx context.Context, plat Platform, version, installDir string) error {
	url, err := plat.AssetURL(version)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-github-cli: installing gh %s from %s\n", version, url)

	dldr, err := download.NewDownloader(ctx, 10*time.Minute, installDir)
	if err != nil {
		return fmt.Errorf("create downloader: %w", err)
	}
	archivePath, err := dldr.Download(ctx, url, 0600)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer os.Remove(archivePath)

	dest := filepath.Join(installDir, version)
	if strings.HasSuffix(url, ".zip") {
		err = extractZip(archivePath, dest)
	} else {
		err = extractTarball(archivePath, dest)
	}
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	return nil
}

// symlinkGH points binDir/gh at src, so gh is on PATH even for processes that do
// not source $BASH_ENV. When binDir is not writable (a machine executor's
// /usr/local/bin), it retries with sudo; a remaining failure is logged, not
// fatal, since $BASH_ENV already carries gh on PATH.
func symlinkGH(src, binDir string) {
	dst := filepath.Join(binDir, "gh")
	_ = os.Remove(dst)
	err := os.Symlink(src, dst)
	if err == nil {
		return
	}
	if !errors.Is(err, os.ErrPermission) {
		fmt.Fprintf(os.Stderr, "setup-github-cli: symlink %s: %v\n", dst, err)
		return
	}
	if out, sudoErr := exec.Command("sudo", "ln", "-sf", src, dst).CombinedOutput(); sudoErr != nil {
		fmt.Fprintf(os.Stderr, "setup-github-cli: symlink %s: %s\n", dst, strings.TrimSpace(string(out)))
	}
}

// stripFirstComponent removes the leading path element of a gh archive entry
// (the "gh_<ver>_<platform>/" wrapper), returning "" for the wrapper itself.
func stripFirstComponent(name string) string {
	name = strings.TrimPrefix(name, "./")
	i := strings.IndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

func extractTarball(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath) //nolint:gosec // path is the archive this package just downloaded
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		rel := stripFirstComponent(hdr.Name)
		if rel == "" {
			continue
		}
		target, err := safeTarget(destDir, rel)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return err
			}
			if err := archive.WriteFile(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		rel := stripFirstComponent(f.Name)
		if rel == "" {
			continue
		}
		target, err := safeTarget(destDir, rel)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		writeErr := archive.WriteFile(target, rc, f.Mode())
		if cerr := rc.Close(); cerr != nil && writeErr == nil {
			writeErr = cerr
		}
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// safeTarget joins rel onto destDir, rejecting entries that would escape destDir
// via "..".
func safeTarget(destDir, rel string) (string, error) {
	target := filepath.Join(destDir, rel)
	destAbs := filepath.Clean(destDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), destAbs) {
		return "", fmt.Errorf("illegal path in archive: %s", rel)
	}
	return target, nil
}
