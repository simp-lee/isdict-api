package isdictapi

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const minimumCommonsVersion = "v0.3.0"

// AC-9: isdict-api 必须升级到包含 commons 上游优化的依赖版本并通过集成编译
func TestCommonsDependencyVersion_IsPinnedAtOrAboveMinimumAndResolvableViaGoList(t *testing.T) {
	t.Helper()

	goModVersion := readRequiredModuleVersion(t, "go.mod", "github.com/simp-lee/isdict-commons")
	if compareModuleVersions(goModVersion, minimumCommonsVersion) < 0 {
		t.Fatalf("go.mod requires github.com/simp-lee/isdict-commons %q, want >= %q", goModVersion, minimumCommonsVersion)
	}

	resolvedVersion := readResolvedModuleVersion(t, "github.com/simp-lee/isdict-commons")
	if resolvedVersion != goModVersion {
		t.Fatalf("go list -m resolved github.com/simp-lee/isdict-commons %q, want %q from go.mod", resolvedVersion, goModVersion)
	}
	if compareModuleVersions(resolvedVersion, minimumCommonsVersion) < 0 {
		t.Fatalf("go list -m resolved github.com/simp-lee/isdict-commons %q, want >= %q", resolvedVersion, minimumCommonsVersion)
	}
}

func readRequiredModuleVersion(t *testing.T, filePath, modulePath string) string {
	t.Helper()

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%q) error = %v", filePath, err)
	}
	t.Cleanup(func() {
		_ = file.Close()
	})

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == modulePath {
			return fields[1]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %q error = %v", filePath, err)
	}

	t.Fatalf("module %q not found in %s", modulePath, filePath)
	return ""
}

func readResolvedModuleVersion(t *testing.T, modulePath string) string {
	t.Helper()

	command := exec.Command("go", "list", "-m", "-f", "{{.Version}}", modulePath)
	command.Dir = "."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -m error = %v; output = %s", err, strings.TrimSpace(string(output)))
	}

	version := strings.TrimSpace(string(output))
	if version == "" {
		t.Fatalf("go list -m returned empty version for %q", modulePath)
	}

	return version
}

func compareModuleVersions(left, right string) int {
	leftParts := parseModuleVersion(left)
	rightParts := parseModuleVersion(right)

	for index := 0; index < len(leftParts) && index < len(rightParts); index++ {
		if leftParts[index] < rightParts[index] {
			return -1
		}
		if leftParts[index] > rightParts[index] {
			return 1
		}
	}

	return 0
}

func parseModuleVersion(version string) [3]int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	core := strings.SplitN(trimmed, "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		panic(fmt.Sprintf("unsupported module version format %q", version))
	}

	var parsed [3]int
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			panic(fmt.Sprintf("unsupported module version format %q: %v", version, err))
		}
		parsed[index] = value
	}

	return parsed
}
