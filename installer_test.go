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
		normalized string
		arch       string
		extension  string
		latest     bool
		useBindEnv bool
		defaultBin bool
		fatZip     bool
	}{
		{name: "tar latest with flag", goos: "Linux", normalized: "linux", arch: "x86_64", extension: ".tar.gz", latest: true},
		{name: "zip explicit with env", goos: "Darwin", normalized: "darwin", arch: "arm64", extension: ".zip", useBindEnv: true, fatZip: true},
		{name: "darwin tar fallback", goos: "Darwin", normalized: "darwin", arch: "x86_64", extension: ".tar.gz"},
		{name: "raw explicit with default", goos: "Linux", normalized: "linux", arch: "aarch64", defaultBin: true},
		{name: "windows zip", goos: "MINGW64_NT-10.0", normalized: "windows", arch: "x86_64", extension: ".zip"},
		{name: "windows tar fallback", goos: "MINGW64_NT-10.0", normalized: "windows", arch: "arm64", extension: ".tar.gz"},
		{name: "windows raw exe", goos: "MSYS_NT-10.0", normalized: "windows", arch: "arm64", extension: ".exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := "v1.2.3"
			assetName := "repo_" + version + "_" + tt.normalized + "_"
			if tt.arch == "x86_64" {
				assetName += "amd64"
			} else {
				assetName += "arm64"
			}
			assetName += tt.extension
			artifact := filepath.Join(t.TempDir(), assetName)
			wantBody := "#!/bin/sh\necho installed\n"
			executableName := "repo"
			if tt.normalized == "windows" {
				executableName += ".exe"
			}
			switch tt.extension {
			case ".tar.gz":
				writeTarGz(t, artifact, []archiveEntry{{name: "repo_" + version + "/" + executableName, body: wantBody}})
			case ".zip":
				writeZip(t, artifact, []archiveEntry{{name: "repo_" + version + "/" + executableName, body: wantBody, fat: tt.fatZip}})
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
			if !strings.Contains(result.output, "build provenance verification unavailable; falling back to SHA-256") {
				t.Fatalf("old gh did not trigger checksum fallback:\n%s", result.output)
			}
			got, err := os.ReadFile(filepath.Join(bindir, executableName))
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

func TestGeneratedInstallerFallsBackWithoutGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh environments")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	env, curlLog := setupArtifact(t, version, assetName, "binary", 0o755)
	fakeBin := envValue(env, "FAKE_BIN")
	if err := os.Remove(filepath.Join(fakeBin, "gh")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"awk", "cat", "cp", "cut", "find", "install", "mkdir", "mktemp",
		"mv", "rm", "sed", "tr", "wc",
	} {
		target, err := exec.LookPath(command)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(fakeBin, command)); err != nil {
			t.Fatal(err)
		}
	}
	hashCommand := ""
	for _, command := range []string{"sha256sum", "shasum", "openssl"} {
		target, err := exec.LookPath(command)
		if err != nil {
			continue
		}
		if err := os.Symlink(target, filepath.Join(fakeBin, command)); err != nil {
			t.Fatal(err)
		}
		hashCommand = command
		break
	}
	if hashCommand == "" {
		t.Fatal("no supported SHA-256 command found")
	}
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + fakeBin
		}
	}
	result := runGeneratedInstaller(t, config{
		repository:      "owner/repo",
		workflow:        "release-build.yaml",
		checksumPattern: "SHA256SUMS",
		verification:    "attestation-or-checksum",
	}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
	if result.err != nil {
		t.Fatalf("installer failed: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.output, "build provenance verification unavailable; falling back to SHA-256") {
		t.Fatalf("missing gh did not trigger checksum fallback:\n%s", result.output)
	}
	logged, err := os.ReadFile(curlLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "/SHA256SUMS") {
		t.Fatalf("checksum was not downloaded:\n%s", logged)
	}
}

func TestGeneratedInstallerAttestationSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	env, curlLog := setupArtifact(t, version, assetName, "binary", 0o755)
	fakeBin := envValue(env, "FAKE_BIN")
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	env = append(env, "GH_LOG="+ghLog)
	writeCommand(t, fakeBin, "git", `
printf '%s\t%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "refs/tags/$FAKE_TAG"
printf '%s\t%s\n' bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb "refs/tags/$FAKE_TAG^{}"
`)
	writeFakeGH(t, fakeBin, 0, ghLog)
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

func TestGeneratedInstallerDebugOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	env, _ := setupArtifact(t, version, assetName, "binary", 0o755)
	result := runGeneratedInstaller(t, config{
		repository:   "owner/repo",
		verification: "none",
	}, env, "-d", "-x", "-b", filepath.Join(t.TempDir(), "bin"), version)
	if result.err != nil {
		t.Fatalf("installer failed: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.output, "owner/repo debug http_download") {
		t.Errorf("-d did not enable debug logging:\n%s", result.output)
	}
	if !strings.Contains(result.output, "+ shift") {
		t.Errorf("-x did not enable shell tracing:\n%s", result.output)
	}
}

func TestGeneratedInstallerUsesAtomicInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh environments")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"
	env, _ := setupArtifact(t, version, assetName, "new", 0o644)
	fakeBin := envValue(env, "FAKE_BIN")
	realInstall, err := exec.LookPath("install")
	if err != nil {
		t.Fatal(err)
	}
	realMV, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "install.log")
	env = append(env,
		"ATOMIC_INSTALL_LOG="+logPath,
		"REAL_INSTALL="+realInstall,
		"REAL_MV="+realMV,
	)
	writeCommand(t, fakeBin, "install", `
printf 'install' >> "$ATOMIC_INSTALL_LOG"
for arg do
	printf '|%s' "$arg" >> "$ATOMIC_INSTALL_LOG"
done
printf '\n' >> "$ATOMIC_INSTALL_LOG"
if [ "${FAIL_ATOMIC_INSTALL:-}" = "1" ] && [ "${1:-}" = "-m" ]; then
	printf 'partial' > "$4"
	exit 1
fi
exec "$REAL_INSTALL" "$@"
`)
	writeCommand(t, fakeBin, "mv", `
printf 'mv' >> "$ATOMIC_INSTALL_LOG"
for arg do
	printf '|%s' "$arg" >> "$ATOMIC_INSTALL_LOG"
done
printf '\n' >> "$ATOMIC_INSTALL_LOG"
exec "$REAL_MV" "$@"
`)
	bindir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(bindir, "repo")
	writeFile(t, destination, "old", 0o755)
	result := runGeneratedInstaller(t, config{
		repository:   "owner/repo",
		verification: "none",
	}, env, "-b", bindir, version)
	if result.err != nil {
		t.Fatalf("installer failed: %v\n%s", result.err, result.output)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("installed body = %q", got)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode = %o", info.Mode().Perm())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logged)), "\n")
	if len(lines) != 3 {
		t.Fatalf("install log = %q", logged)
	}
	if !strings.HasPrefix(lines[0], "install|-d|") {
		t.Fatalf("destination directory was not created with install: %q", lines[0])
	}
	prepare := strings.Split(lines[1], "|")
	if len(prepare) != 5 || prepare[0] != "install" || prepare[1] != "-m" || prepare[2] != "0755" {
		t.Fatalf("binary was not prepared with install -m 0755: %q", lines[1])
	}
	staging := prepare[4]
	if filepath.Dir(staging) != bindir || staging == destination {
		t.Fatalf("staging path %q is not a distinct file in %q", staging, bindir)
	}
	commit := strings.Split(lines[2], "|")
	if len(commit) != 4 || commit[0] != "mv" || commit[1] != "-f" ||
		commit[2] != staging || commit[3] != destination {
		t.Fatalf("destination was not atomically replaced from staging: %q", lines[2])
	}

	failureBindir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(failureBindir, 0o755); err != nil {
		t.Fatal(err)
	}
	failureDestination := filepath.Join(failureBindir, "repo")
	writeFile(t, failureDestination, "existing", 0o755)
	failureEnv := append(env, "FAIL_ATOMIC_INSTALL=1")
	failure := runGeneratedInstaller(t, config{
		repository:   "owner/repo",
		verification: "none",
	}, failureEnv, "-b", failureBindir, version)
	if failure.err == nil || !strings.Contains(failure.output, "could not prepare repo") {
		t.Fatalf("result = %v\n%s", failure.err, failure.output)
	}
	unchanged, err := os.ReadFile(failureDestination)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "existing" {
		t.Fatalf("failed install changed destination to %q", unchanged)
	}
	stagingFiles, err := filepath.Glob(filepath.Join(failureBindir, ".repo.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stagingFiles) != 0 {
		t.Fatalf("failed install left staging files: %v", stagingFiles)
	}
}

func TestGeneratedInstallerMultipleBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh environments")
	}
	tests := []struct {
		name       string
		unameOS    string
		normalized string
		arch       string
		extension  string
	}{
		{name: "linux tar", unameOS: "Linux", normalized: "linux", arch: "x86_64", extension: ".tar.gz"},
		{name: "windows zip", unameOS: "MINGW64_NT-10.0", normalized: "windows", arch: "arm64", extension: ".zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const version = "v1.2.3"
			assetName := "tools_" + version + "_" + tt.normalized + "_"
			if tt.arch == "x86_64" {
				assetName += "amd64"
			} else {
				assetName += "arm64"
			}
			assetName += tt.extension
			suffix := ""
			if tt.normalized == "windows" {
				suffix = ".exe"
			}
			entries := []archiveEntry{
				{name: "tools_" + version + "/foo" + suffix, body: "foo"},
				{name: "tools_" + version + "/bin/bar" + suffix, body: "bar"},
			}
			artifact := filepath.Join(t.TempDir(), assetName)
			if tt.extension == ".zip" {
				writeZip(t, artifact, entries)
			} else {
				writeTarGz(t, artifact, entries)
			}
			env, _ := installerEnvironment(t, artifact, assetName, version)
			env = append(env, "FAKE_UNAME_S="+tt.unameOS, "FAKE_UNAME_M="+tt.arch)
			bindir := filepath.Join(t.TempDir(), "bin")
			result := runGeneratedInstaller(t, config{
				repository:      "owner/repo",
				name:            "tools",
				binaries:        []string{"foo", "bar"},
				checksumPattern: "SHA256SUMS",
				verification:    "checksum",
			}, env, "-b", bindir, version)
			if result.err != nil {
				t.Fatalf("installer failed: %v\n%s", result.err, result.output)
			}
			for binary, want := range map[string]string{"foo" + suffix: "foo", "bar" + suffix: "bar"} {
				got, err := os.ReadFile(filepath.Join(bindir, binary))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != want {
					t.Errorf("%s body = %q, want %q", binary, got, want)
				}
			}
		})
	}
}

func TestGeneratedInstallerMultipleBinaryFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh environments")
	}
	const version = "v1.2.3"
	t.Run("missing binary leaves destinations untouched", func(t *testing.T) {
		assetName := "tools_" + version + "_linux_amd64.tar.gz"
		artifact := filepath.Join(t.TempDir(), assetName)
		writeTarGz(t, artifact, []archiveEntry{{name: "tools/foo", body: "foo"}})
		env, _ := installerEnvironment(t, artifact, assetName, version)
		bindir := filepath.Join(t.TempDir(), "bin")
		if err := os.MkdirAll(bindir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(bindir, "foo"), "existing", 0o755)
		result := runGeneratedInstaller(t, config{
			repository:   "owner/repo",
			name:         "tools",
			binaries:     []string{"foo", "bar"},
			verification: "none",
		}, env, "-b", bindir, version)
		if result.err == nil || !strings.Contains(result.output, "exactly one executable named bar") {
			t.Fatalf("result = %v\n%s", result.err, result.output)
		}
		got, err := os.ReadFile(filepath.Join(bindir, "foo"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "existing" {
			t.Fatalf("foo changed before all binaries were prepared: %q", got)
		}
	})

	t.Run("raw artifact rejects multiple binaries", func(t *testing.T) {
		assetName := "tools_" + version + "_linux_amd64"
		artifact := filepath.Join(t.TempDir(), assetName)
		writeFile(t, artifact, "raw", 0o755)
		env, _ := installerEnvironment(t, artifact, assetName, version)
		result := runGeneratedInstaller(t, config{
			repository:   "owner/repo",
			name:         "tools",
			binaries:     []string{"foo", "bar"},
			verification: "none",
		}, env, "-b", filepath.Join(t.TempDir(), "bin"), version)
		if result.err == nil || !strings.Contains(result.output, "raw artifacts can install exactly one binary") {
			t.Fatalf("result = %v\n%s", result.err, result.output)
		}
	})
}

func TestGeneratedInstallerDoesNotDowngradeFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	assetName := "repo_" + version + "_linux_amd64"

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
			env, curlLog := setupArtifact(t, version, assetName, "binary", 0o755)
			fakeBin := envValue(env, "FAKE_BIN")
			writeCommand(t, fakeBin, "git", tt.gitBody)
			writeFakeGH(t, fakeBin, tt.verifyExit, "")

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
			env, _ := setupArtifact(t, version, assetName, "binary", 0o755)
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

func TestGeneratedInstallerArchivePriority(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated installers target POSIX sh on Linux and macOS")
	}
	const version = "v1.2.3"
	tests := []struct {
		name       string
		unameOS    string
		normalized string
		executable string
		preferred  string
		wantBody   string
	}{
		{name: "linux prefers tar", unameOS: "Linux", normalized: "linux", executable: "repo", preferred: ".tar.gz", wantBody: "tar"},
		{name: "darwin prefers zip", unameOS: "Darwin", normalized: "darwin", executable: "repo", preferred: ".zip", wantBody: "zip"},
		{name: "windows prefers zip", unameOS: "MINGW64_NT-10.0", normalized: "windows", executable: "repo.exe", preferred: ".zip", wantBody: "zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assetBase := "repo_" + version + "_" + tt.normalized + "_amd64"
			tarName := assetBase + ".tar.gz"
			tarArtifact := filepath.Join(t.TempDir(), tarName)
			writeTarGz(t, tarArtifact, []archiveEntry{{name: tt.executable, body: "tar"}})
			zipName := assetBase + ".zip"
			zipArtifact := filepath.Join(t.TempDir(), zipName)
			writeZip(t, zipArtifact, []archiveEntry{{name: tt.executable, body: "zip"}})
			env, curlLog := installerEnvironment(t, tarArtifact, tarName, version)
			env = append(env,
				"ARTIFACT_NAME_2="+zipName,
				"ARTIFACT_2="+zipArtifact,
				"FAKE_UNAME_S="+tt.unameOS,
			)
			bindir := filepath.Join(t.TempDir(), "bin")
			result := runGeneratedInstaller(t, config{
				repository:   "owner/repo",
				verification: "none",
			}, env, "-b", bindir, version)
			if result.err != nil {
				t.Fatalf("installer failed: %v\n%s", result.err, result.output)
			}
			got, err := os.ReadFile(filepath.Join(bindir, tt.executable))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantBody {
				t.Fatalf("installed body = %q, want %q", got, tt.wantBody)
			}
			logged, err := os.ReadFile(curlLog)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(logged), assetBase+tt.preferred) {
				t.Fatalf("preferred artifact was not requested:\n%s", logged)
			}
			other := ".zip"
			if tt.preferred == ".zip" {
				other = ".tar.gz"
			}
			if strings.Contains(string(logged), assetBase+other) {
				t.Fatalf("lower-priority artifact was requested after success:\n%s", logged)
			}
		})
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
			name:      "zip Windows path traversal",
			extension: ".zip",
			entries:   []archiveEntry{{name: `..\repo`, body: "binary"}},
			want:      "Windows path separator",
		},
		{
			name:      "zip drive-qualified path",
			extension: ".zip",
			entries:   []archiveEntry{{name: "C:/repo", body: "binary"}},
			want:      "drive-qualified path",
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

// setupArtifact writes a single-file artifact and wires up the fake
// installer environment for it, returning the environment and the path to
// the curl invocation log.
func setupArtifact(t *testing.T, version, assetName, body string, mode os.FileMode) ([]string, string) {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), assetName)
	writeFile(t, artifact, body, mode)
	return installerEnvironment(t, artifact, assetName, version)
}

// writeFakeGH installs a fake gh command that reports the given version,
// supports --signer-digest attestation verification, and otherwise logs its
// arguments to ghLog (if non-empty) before exiting with verifyExit.
func writeFakeGH(t *testing.T, dir string, verifyExit int, ghLog string) {
	t.Helper()
	const header = `
if [ "$1" = "--version" ]; then
	printf 'gh version 2.93.0 (test)\n'
	exit 0
fi
if [ "$1" = "attestation" ] && [ "$2" = "verify" ] && [ "$3" = "--help" ]; then
	printf '%s\n' '--signer-digest'
	exit 0
fi
`
	if ghLog == "" {
		writeCommand(t, dir, "gh", header+fmt.Sprintf("exit %d", verifyExit))
		return
	}
	writeCommand(t, dir, "gh", header+fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\nexit %d", shellQuote(ghLog), verifyExit))
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
