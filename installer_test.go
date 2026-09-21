package insmith

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type archiveEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
	fat      bool
}

func TestGeneratedInstallerFormats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	tests := []struct {
		name       string
		goos       string
		arch       string
		extension  string
		latest     bool
		useBindEnv bool
		defaultBin bool
		fatZip     bool
	}{
		{name: "tar latest with flag", goos: "Linux", arch: "x86_64", extension: ".tar.gz", latest: true},
		{name: "zip explicit with env", goos: "Darwin", arch: "arm64", extension: ".zip", useBindEnv: true, fatZip: true},
		{name: "raw explicit with default", goos: "Linux", arch: "aarch64", defaultBin: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := "v1.2.3"
			assetName := "repo_" + version + "_" + strings.ToLower(tt.goos) + "_"
			if tt.arch == "x86_64" {
				assetName += "amd64"
			} else {
				assetName += "arm64"
			}
			assetName += tt.extension
			artifact := filepath.Join(t.TempDir(), assetName)
			wantBody := "#!/bin/sh\necho installed\n"
			switch tt.extension {
			case ".tar.gz":
				writeTarGz(t, artifact, []archiveEntry{{name: "repo_" + version + "/repo", body: wantBody}})
			case ".zip":
				writeZip(t, artifact, []archiveEntry{{name: "repo_" + version + "/repo", body: wantBody, fat: tt.fatZip}})
			default:
				writeFile(t, artifact, wantBody, 0o755)
			}

			env, curlLog := installerEnvironment(t, artifact, assetName, version)
			bindir := filepath.Join(t.TempDir(), "bin")
			args := []string{"-b", bindir, version}
			if tt.latest {
				args = []string{"-b", bindir}
			}
			if tt.useBindEnv {
				env = append(env, "BINDIR="+bindir)
				args = []string{version}
			}
			if tt.defaultBin {
				workDir := t.TempDir()
				bindir = filepath.Join(workDir, "bin")
				env = append(env, "COMMAND_DIR="+workDir)
				args = []string{version}
			}
			env = append(env, "FAKE_UNAME_S="+tt.goos, "FAKE_UNAME_M="+tt.arch)

			result := runGeneratedInstaller(t, config{
				repository:      "owner/repo",
				checksumPattern: "SHA256SUMS",
				verification:    "attestation-or-checksum",
			}, env, args...)
			if result.err != nil {
				t.Fatalf("installer failed: %v\n%s", result.err, result.output)
			}
			got, err := os.ReadFile(filepath.Join(bindir, "repo"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != wantBody {
				t.Fatalf("installed body = %q", got)
			}
			logged, err := os.ReadFile(curlLog)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(logged), assetName) ||
				!strings.Contains(string(logged), "SHA256SUMS") {
				t.Fatalf("curl log = %s", logged)
			}
		})
	}
}

func TestGeneratedInstallerAttestationSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	artifact := filepath.Join(t.TempDir(), assetName)
	writeFile(t, artifact, "binary", 0o755)
	env, curlLog := installerEnvironment(t, artifact, assetName, version)
	fakeBin := envValue(env, "FAKE_BIN")
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	env = append(env, "GH_LOG="+ghLog)
	writeCommand(t, fakeBin, "git", `
printf '%s\t%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "refs/tags/$FAKE_TAG"
printf '%s\t%s\n' bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb "refs/tags/$FAKE_TAG^{}"
`)
	writeCommand(t, fakeBin, "gh", `
if [ "$1" = "--version" ]; then
	printf 'gh version 2.93.0 (test)\n'
	exit 0
fi
if [ "$1" = "attestation" ] && [ "$2" = "verify" ] && [ "$3" = "--help" ]; then
	printf '%s\n' '--signer-digest'
	exit 0
fi
printf '%s\n' "$*" > "$GH_LOG"
exit 0
`)
	result := runGeneratedInstaller(t, config{
		repository:   "owner/repo",
		workflow:     "release-build.yaml",
		verification: "attestation",
	}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
	if result.err != nil {
		t.Fatalf("installer failed: %v\n%s", result.err, result.output)
	}
	logged, err := os.ReadFile(curlLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logged), "/SHA256SUMS") {
		t.Fatalf("successful attestation downloaded checksums:\n%s", logged)
	}
	verified, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(verified), "--source-digest bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") ||
		!strings.Contains(string(verified), "--signer-digest bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("attestation did not use peeled tag commit: %s", verified)
	}
}

func TestGeneratedInstallerDoesNotDowngradeFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	artifact := filepath.Join(t.TempDir(), assetName)
	writeFile(t, artifact, "binary", 0o755)

	tests := []struct {
		name       string
		gitBody    string
		verifyExit int
		want       string
	}{
		{name: "tag resolution failure", gitBody: "exit 1", want: "could not resolve release tag"},
		{
			name:       "attestation failure",
			gitBody:    `printf '%s\t%s\n' 0123456789012345678901234567890123456789 "refs/tags/$FAKE_TAG"`,
			verifyExit: 1,
			want:       "attestation verification failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, curlLog := installerEnvironment(t, artifact, assetName, version)
			fakeBin := envValue(env, "FAKE_BIN")
			writeCommand(t, fakeBin, "git", tt.gitBody)
			writeCommand(t, fakeBin, "gh", fmt.Sprintf(`
if [ "$1" = "--version" ]; then
	printf 'gh version 2.93.0 (test)\n'
	exit 0
fi
if [ "$1" = "attestation" ] && [ "$2" = "verify" ] && [ "$3" = "--help" ]; then
	printf '%%s\n' '--signer-digest'
	exit 0
fi
exit %d
`, tt.verifyExit))

			result := runGeneratedInstaller(t, config{
				repository:      "owner/repo",
				checksumPattern: "SHA256SUMS",
				verification:    "attestation-or-checksum",
			}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
			if result.err == nil || !strings.Contains(result.output, tt.want) {
				t.Fatalf("result = %v\n%s", result.err, result.output)
			}
			logged, err := os.ReadFile(curlLog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(logged), "/SHA256SUMS") {
				t.Fatalf("verification failure downgraded to checksum:\n%s", logged)
			}
		})
	}
}

func TestGeneratedInstallerRejectsInvalidChecksums(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	artifact := filepath.Join(t.TempDir(), assetName)
	writeFile(t, artifact, "binary", 0o755)
	validHash := fmt.Sprintf("%x", sha256.Sum256([]byte("binary")))
	tests := []struct {
		name      string
		checksums string
		want      string
	}{
		{name: "missing", checksums: validHash + "  other\n", want: "unable to find checksum"},
		{
			name:      "duplicate",
			checksums: validHash + "  " + assetName + "\n" + validHash + "  ./" + assetName + "\n",
			want:      "multiple checksums",
		},
		{name: "mismatch", checksums: strings.Repeat("0", 64) + "  " + assetName + "\n", want: "checksum verification failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := installerEnvironment(t, artifact, assetName, version)
			writeFile(t, envValue(env, "CHECKSUMS"), tt.checksums, 0o644)
			result := runGeneratedInstaller(t, config{
				repository:      "owner/repo",
				checksumPattern: "SHA256SUMS",
				verification:    "checksum",
			}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
			if result.err == nil || !strings.Contains(result.output, tt.want) {
				t.Fatalf("result = %v\n%s", result.err, result.output)
			}
		})
	}
}

func TestGeneratedInstallerRejectsAmbiguousAssets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64.tar.gz"
	artifact := filepath.Join(t.TempDir(), assetName)
	writeTarGz(t, artifact, []archiveEntry{{name: "repo", body: "binary"}})
	env, _ := installerEnvironment(t, artifact, assetName, version)
	assetName2 := "repo_" + version + "_linux_amd64.zip"
	artifact2 := filepath.Join(t.TempDir(), assetName2)
	writeZip(t, artifact2, []archiveEntry{{name: "repo", body: "binary"}})
	env = append(env, "ARTIFACT_NAME_2="+assetName2, "ARTIFACT_2="+artifact2)
	result := runGeneratedInstaller(t, config{
		repository:   "owner/repo",
		verification: "none",
	}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
	if result.err == nil || !strings.Contains(result.output, "unique asset") {
		t.Fatalf("result = %v\n%s", result.err, result.output)
	}
}

func TestGeneratedInstallerRejectsUnsafeArchives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	tests := []struct {
		name      string
		extension string
		entries   []archiveEntry
		want      string
	}{
		{
			name:      "tar path traversal",
			extension: ".tar.gz",
			entries:   []archiveEntry{{name: "../repo", body: "binary"}},
			want:      "path traversal",
		},
		{
			name:      "tar symlink",
			extension: ".tar.gz",
			entries: []archiveEntry{{
				name:     "repo",
				typeflag: tar.TypeSymlink,
				linkname: "/tmp/target",
			}},
			want: "symlink",
		},
		{
			name:      "tar duplicate binary",
			extension: ".tar.gz",
			entries: []archiveEntry{
				{name: "one/repo", body: "one"},
				{name: "two/repo", body: "two"},
			},
			want: "exactly one executable",
		},
		{
			name:      "zip path traversal",
			extension: ".zip",
			entries:   []archiveEntry{{name: "../repo", body: "binary"}},
			want:      "path traversal",
		},
		{
			name:      "zip symlink",
			extension: ".zip",
			entries: []archiveEntry{{
				name:     "repo",
				typeflag: tar.TypeSymlink,
				linkname: "/tmp/target",
			}},
			want: "symlink",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const version = "v1.2.3"
			assetName := "repo_" + version + "_linux_amd64" + tt.extension
			artifact := filepath.Join(t.TempDir(), assetName)
			if tt.extension == ".zip" {
				writeZip(t, artifact, tt.entries)
			} else {
				writeTarGz(t, artifact, tt.entries)
			}
			env, _ := installerEnvironment(t, artifact, assetName, version)
			result := runGeneratedInstaller(t, config{
				repository:   "owner/repo",
				verification: "none",
			}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
			if result.err == nil || !strings.Contains(result.output, tt.want) {
				t.Fatalf("result = %v\n%s", result.err, result.output)
			}
		})
	}
}

func TestTruncatedGeneratedInstallerDoesNotExecute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	script, err := generate(config{
		repository:   "owner/repo",
		verification: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	script = strings.TrimSuffix(script, "main \"$@\"\n")
	dir := t.TempDir()
	marker := filepath.Join(dir, "curl-ran")
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCommand(t, fakeBin, "curl", `: > "$CURL_MARKER"; exit 1`)
	scriptPath := filepath.Join(dir, "install.sh")
	writeFile(t, scriptPath, script, 0o755)
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":/usr/bin:/bin", "CURL_MARKER="+marker)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("truncated installer failed to parse: %v\n%s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("truncated installer executed curl: %v", err)
	}
}

type installerResult struct {
	output string
	err    error
}

func runGeneratedInstaller(t *testing.T, cfg config, env []string, args ...string) installerResult {
	t.Helper()
	script, err := generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(t.TempDir(), "install.sh")
	writeFile(t, scriptPath, script, 0o755)
	cmd := exec.Command("/bin/sh", append([]string{scriptPath}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if commandDir := envValue(env, "COMMAND_DIR"); commandDir != "" {
		cmd.Dir = commandDir
	}
	output, err := cmd.CombinedOutput()
	return installerResult{output: string(output), err: err}
}

func installerEnvironment(t *testing.T, artifact, assetName, version string) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	curlLog := filepath.Join(dir, "curl.log")
	checksums := filepath.Join(dir, "SHA256SUMS")
	sum := sha256File(t, artifact)
	writeFile(t, checksums, fmt.Sprintf("%x  %s\n", sum, assetName), 0o644)
	writeCommand(t, fakeBin, "uname", `
case "$1" in
	-s) printf '%s\n' "$FAKE_UNAME_S" ;;
	-m) printf '%s\n' "$FAKE_UNAME_M" ;;
	*) exit 1 ;;
esac
`)
	writeCommand(t, fakeBin, "gh", `
if [ "$1" = "--version" ]; then
	printf 'gh version 2.92.0 (test)\n'
	exit 0
fi
exit 1
`)
	writeCommand(t, fakeBin, "curl", `
out=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) out=$2; shift 2 ;;
		--proto|-H) shift 2 ;;
		--tlsv1.2|-fsSL) shift ;;
		*) url=$1; shift ;;
	esac
done
printf '%s\n' "$url" >> "$CURL_LOG"
case "$url" in
	*/releases/latest|*/releases/"$FAKE_TAG")
		printf '{"tag_name":"%s"}\n' "$FAKE_TAG" > "$out"
		;;
	*/releases/download/*/"$ARTIFACT_NAME") cp "$ARTIFACT" "$out" ;;
	*/releases/download/*/"$ARTIFACT_NAME_2")
		[ -n "${ARTIFACT_2:-}" ] || exit 1
		cp "$ARTIFACT_2" "$out"
		;;
	*/releases/download/*/SHA256SUMS) cp "$CHECKSUMS" "$out" ;;
	*) exit 1 ;;
esac
`)
	path := fakeBin + ":/usr/bin:/bin:/usr/sbin:/sbin"
	env := []string{
		"PATH=" + path,
		"BINDIR=",
		"FAKE_BIN=" + fakeBin,
		"FAKE_UNAME_S=Linux",
		"FAKE_UNAME_M=x86_64",
		"FAKE_TAG=" + version,
		"ARTIFACT=" + artifact,
		"ARTIFACT_NAME=" + assetName,
		"ARTIFACT_NAME_2=",
		"CHECKSUMS=" + checksums,
		"CURL_LOG=" + curlLog,
	}
	return env, curlLog
}

func envValue(env []string, name string) string {
	prefix := name + "="
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

func writeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), "#!/bin/sh\nset -eu\n"+body+"\n", 0o755)
}

func writeTarGz(t *testing.T, path string, entries []archiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0o755,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg {
			if _, err := io.WriteString(tarWriter, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, entries []archiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.typeflag == tar.TypeSymlink {
			header.SetMode(os.ModeSymlink | 0o777)
		} else if !entry.fat {
			header.SetMode(0o755)
		}
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		body := entry.body
		if entry.typeflag == tar.TypeSymlink {
			body = entry.linkname
		}
		if _, err := io.WriteString(entryWriter, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func sha256File(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(content)
}
