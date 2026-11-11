package main

import (
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

// These keep the helpers' local names at their many call sites. The helpers
// themselves are in internal/acceptance.

const availableConfiguration = acceptance.AvailableConfiguration

type (
	configurationRun = acceptance.Run
	readinessEntry   = acceptance.ReadinessEntry
)

func sharedExecutable(t *testing.T) string {
	t.Helper()
	return acceptance.SharedExecutable(t)
}

func sharedNoCGOExecutable(t *testing.T) string {
	t.Helper()
	return acceptance.SharedNoCGOExecutable(t)
}

func buildConfigurationExecutable(t *testing.T, overlay string) string {
	t.Helper()
	return acceptance.BuildExecutable(t, overlay)
}

func buildConfigurationFixtureExecutable(t *testing.T) string {
	t.Helper()
	return acceptance.BuildProviderFixtureExecutable(t)
}

func configurationProject(t *testing.T) string {
	t.Helper()
	return acceptance.Project(t)
}

func putConfigurationFile(t *testing.T, root, name, contents string) {
	t.Helper()
	acceptance.PutFile(t, root, name, contents)
}

func cleanConfiguration() string { return acceptance.CleanConfiguration() }

func configurationBytes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	return acceptance.ConfigurationBytes(t, root)
}

func spacingDigest(text string) string { return acceptance.Digest(text) }

func spacingSaved(t *testing.T, root string) string {
	t.Helper()
	return acceptance.Saved(t, root)
}

func runConfigurationProcess(t *testing.T, binary, cwd string, args ...string) configurationRun {
	t.Helper()
	return acceptance.RunProcess(t, binary, cwd, args...)
}

func runConfigurationProcessEnv(t *testing.T, binary, cwd string, env []string, args ...string) configurationRun {
	t.Helper()
	return acceptance.RunProcessEnv(t, binary, cwd, env, args...)
}

func validateConfigurationOperationJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	return acceptance.ValidateOperationJSON(t, data, operation)
}

func runInspection(t *testing.T, binary, cwd, snapshotRoot string, args ...string) (configurationRun, inspection.Projection) {
	t.Helper()
	return acceptance.RunInspection(t, binary, cwd, snapshotRoot, args...)
}

func readinessHash(s string) string { return acceptance.ReadinessHash(s) }

func readinessState(t *testing.T, root string) map[string]readinessEntry {
	t.Helper()
	return acceptance.ReadinessState(t, root)
}

func readinessDifferences(a, b map[string]readinessEntry) []string {
	return acceptance.ReadinessDifferences(a, b)
}

func readinessAssertEqual(t *testing.T, checkpoint string, a, b map[string]readinessEntry) {
	t.Helper()
	acceptance.ReadinessAssertEqual(t, checkpoint, a, b)
}

func md038NativeRefusal(t *testing.T, got configurationRun, out result.Result) bool {
	t.Helper()
	return acceptance.MD038NativeRefusal(t, got, out)
}
