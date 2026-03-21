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

const (
	minimumCommonsVersion = "v0.4.0"
	minimumDataVersion    = "v0.1.1"
)

// AC-9: isdict-api 必须升级到包含上游注解字段优化的依赖版本并通过集成编译
func TestDependencyVersions_ArePinnedAtOrAboveMinimumAndResolvableViaGoList(t *testing.T) {
	t.Helper()

	tests := []struct {
		name           string
		modulePath     string
		minimumVersion string
	}{
		{
			name:           "commons",
			modulePath:     "github.com/simp-lee/isdict-commons",
			minimumVersion: minimumCommonsVersion,
		},
		{
			name:           "data",
			modulePath:     "github.com/simp-lee/isdict-data",
			minimumVersion: minimumDataVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goModVersion := readRequiredModuleVersion(t, "go.mod", tt.modulePath)
			if compareModuleVersions(goModVersion, tt.minimumVersion) < 0 {
				t.Fatalf("go.mod requires %s %q, want >= %q", tt.modulePath, goModVersion, tt.minimumVersion)
			}

			resolvedVersion := readResolvedModuleVersion(t, tt.modulePath)
			if resolvedVersion != goModVersion {
				t.Fatalf("go list -m resolved %s %q, want %q from go.mod", tt.modulePath, resolvedVersion, goModVersion)
			}
			if compareModuleVersions(resolvedVersion, tt.minimumVersion) < 0 {
				t.Fatalf("go list -m resolved %s %q, want >= %q", tt.modulePath, resolvedVersion, tt.minimumVersion)
			}
		})
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
