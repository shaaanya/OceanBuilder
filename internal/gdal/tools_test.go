package gdal

import (
	"strings"
	"testing"
)

func TestCheckToolsNamesEveryMissingProgram(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // an empty directory: nothing resolves
	err := CheckTools()
	if err == nil {
		t.Fatal("want an error with an empty PATH")
	}
	for _, tool := range RequiredTools {
		if !strings.Contains(err.Error(), tool) {
			t.Errorf("error does not name %s: %v", tool, err)
		}
	}
	if !strings.Contains(err.Error(), "Install GDAL") {
		t.Errorf("error gives no way forward: %v", err)
	}
}
