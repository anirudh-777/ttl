package update

import "testing"

func TestSemverLess(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		// Equal
		{"0.4.1", "0.4.1", false},
		{"v0.4.1", "v0.4.1", false},
		// Strict ordering
		{"0.3.0", "0.4.1", true},
		{"0.4.1", "0.3.0", false},
		// Multi-digit segments
		{"0.4.1", "0.10.0", true},
		{"0.10.0", "0.4.1", false},
		{"1.0.0", "0.99.99", false},
		{"0.99.99", "1.0.0", true},
		// v prefix
		{"v0.4.1", "v0.4.2", true},
		{"v0.4.2", "v0.4.1", false},
		{"0.4.1", "v0.4.2", true},
		// Pre-release (semver §11: pre-release sorts before release)
		{"0.4.1-rc1", "0.4.1", true},
		{"0.4.1", "0.4.1-rc1", false},
		{"0.4.1-rc1", "0.4.1-rc2", true},
		// §11.4.4: identifiers compared lexically; "rc10" < "rc2"
		{"0.4.1-rc2", "0.4.1-rc10", false},
		{"0.4.1-rc10", "0.4.1-rc2", true},
		// Empty
		{"", "0.4.1", true},
		{"0.4.1", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got := semverLess(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("semverLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestPlatform(t *testing.T) {
	// Just verify the format. runtime.GOOS and GOARCH are stable.
	got := Platform()
	if got != runtimeGOOS()+"-"+runtimeGOARCH() {
		t.Errorf("Platform() = %q, want %q", got, runtimeGOOS()+"-"+runtimeGOARCH())
	}
}

func TestCachePath(t *testing.T) {
	p, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Fatal("empty cache path")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		wantNum []int
		wantPre bool
		wantTag string
	}{
		{"0.4.1", []int{0, 4, 1}, false, ""},
		{"1.0.0", []int{1, 0, 0}, false, ""},
		{"0.4.1-rc1", []int{0, 4, 1}, true, "rc1"},
		{"0.4.1-beta.2", []int{0, 4, 1}, true, "beta.2"},
		{"10.20.30", []int{10, 20, 30}, false, ""},
	}
	for _, tc := range tests {
		gotNum, gotPre, gotTag := parseVersion(tc.in)
		if !equalInts(gotNum, tc.wantNum) || gotPre != tc.wantPre || gotTag != tc.wantTag {
			t.Errorf("parseVersion(%q) = (%v, %v, %q), want (%v, %v, %q)",
				tc.in, gotNum, gotPre, gotTag, tc.wantNum, tc.wantPre, tc.wantTag)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
