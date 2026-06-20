package services

import "testing"

func TestProviderFilePathRejectsUnsafeCustomKind(t *testing.T) {
	tests := []string{
		"custom:../outside",
		"custom:..\\outside",
		"custom:/absolute",
		"custom:a/b",
		"custom:a:b",
		"custom:",
	}

	for _, kind := range tests {
		t.Run(kind, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			t.Setenv("USERPROFILE", tmpHome)

			if _, err := providerFilePath(kind); err == nil {
				t.Fatalf("expected unsafe kind %q to fail", kind)
			}
		})
	}
}

func TestProviderFilePathAcceptsSafeCustomKind(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	if _, err := providerFilePath("custom:tool_01-safe"); err != nil {
		t.Fatalf("expected safe custom kind to pass: %v", err)
	}
}
