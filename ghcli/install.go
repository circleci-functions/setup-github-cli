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
	"github.com/circleci-functions/toolkit/download"
	"github.com/circleci-functions/toolkit/pathlink"
)

// DefaultBaseURL is the GitHub base the gh releases are fetched from.
const DefaultBaseURL = "https://github.com"

// ResolveVersion turns a version spec into a concrete semver tag ("v2.37.0").
// "latest" (or "") is resolved from baseURL's releases page, retrying up to
// maxRetries times; any other spec must be a full X.Y.Z release, with or
// without a leading "v", since that is the only form gh tags its releases with.
func ResolveVersion(ctx context.Context, baseURL, spec string, maxRetries int) (string, error) {
	if spec != "" && spec != "latest" {
		v := spec
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		if semver.Canonical(v) != v || semver.Prerelease(v) != "" {
			return "", fmt.Errorf("invalid version %q: use a full X.Y.Z release such as 2.62.0, or \"latest\"", spec)
		}
		return v, nil
	}

	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tag, err := latestTag(ctx, baseURL)
		if err == nil {
			return tag, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("resolve latest gh release after %d retries: %w", maxRetries, lastErr)
}

// latestTag follows the releases/latest redirect and reads the tag from the
// final URL (.../releases/tag/v2.37.0).
func latestTag(ctx context.Context, baseURL string) (string, error) {
	url := strings.TrimRight(baseURL, "/") + "/cli/cli/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	// BinDir links gh into exactly this directory and skips PATH discovery.
	// Tests set it, so they cannot write to the real PATH.
	BinDir string
	// BaseURL is the GitHub base assets are downloaded from; DefaultBaseURL
	// when empty.
	BaseURL string
}

// EnsureInstalled installs gh at version (a semver tag) for the current platform
// under opts.InstallDir, links it into a directory already on PATH so later job
// steps resolve it in any shell, and returns the path to the gh binary. A
// version already present under InstallDir is reused rather than downloaded
// again.
func EnsureInstalled(ctx context.Context, version string, opts InstallOptions) (string, error) {
	plat, err := DetectPlatform()
	if err != nil {
		return "", err
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	ghBin := filepath.Join(opts.InstallDir, version, "bin", "gh")
	if installedVersion(ghBin) == version {
		fmt.Fprintf(os.Stderr, "setup-github-cli: gh %s already installed at %s, skipping download\n", version, ghBin)
	} else if err := install(ctx, plat, baseURL, version, opts.InstallDir); err != nil {
		return "", err
	}

	res, err := pathlink.Ensure(pathlink.Options{
		Names:     []string{"gh"},
		FromDir:   filepath.Dir(ghBin),
		Preferred: pathlink.UserBinDirs(),
		Dir:       opts.BinDir,
		Verify:    verifyGH,
	})
	if err != nil {
		return "", fmt.Errorf("link gh onto PATH: %w", err)
	}
	fmt.Fprintf(os.Stderr, "setup-github-cli: gh resolves from %s\n", res.Dir)
	return ghBin, nil
}

// verifyGH confirms the binary PATH resolves is gh, catching a link that points
// at a lookalike of the same name.
func verifyGH(bin string) error {
	out, err := pathlink.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not report its version: %w", err)
	}
	if !strings.Contains(string(out), "gh version") {
		return fmt.Errorf("reports %q, not a gh version", strings.TrimSpace(string(out)))
	}
	return nil
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

func install(ctx context.Context, plat Platform, baseURL, version, installDir string) error {
	url, err := plat.AssetURL(baseURL, version)
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
