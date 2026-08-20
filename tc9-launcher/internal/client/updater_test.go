package client

import (
	"io"
	"strings"
	"testing"
)

func TestValidateManagedPath(t *testing.T) {
	allowed := []string{
		"Data/patch-T.MPQ",
		"Interface/AddOns/SWP/SWP.lua",
		"Interface/AddOns/SWPMultispecs/SWPMultispecs.toc",
		"Interface/AddOns/SWPMultispecs/SWPMultispecs.lua",
		"Interface/AddOns/SWPHeroicUI/SWPHeroicUI.toc",
		"Interface/AddOns/SWPHeroicUI/SWPHeroicUI.lua",
	}
	for _, path := range allowed {
		if err := validateManagedPath(path); err != nil {
			t.Errorf("expected %q to be allowed: %v", path, err)
		}
	}

	rejected := []string{
		"Wow.exe",
		"../Interface/AddOns/SWPMultispecs/SWPMultispecs.lua",
		"Interface/AddOns/Unmanaged/file.lua",
	}
	for _, path := range rejected {
		if err := validateManagedPath(path); err == nil {
			t.Errorf("expected %q to be rejected", path)
		}
	}
}

func TestProgressReaderReportsCumulativeBytes(t *testing.T) {
	var reported []int64
	reader := &progressReader{
		reader: strings.NewReader("launcher update"),
		progress: func(downloaded int64) {
			reported = append(reported, downloaded)
		},
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "launcher update" {
		t.Fatalf("unexpected downloaded data %q", data)
	}
	if len(reported) == 0 || reported[len(reported)-1] != int64(len(data)) {
		t.Fatalf("final progress = %v, want %d", reported, len(data))
	}
}
