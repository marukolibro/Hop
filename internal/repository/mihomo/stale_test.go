package mihomo

import "testing"

// TestStartedWithHome pins the guard that keeps a reused pid from being
// mistaken for Hop's core: only the exact -d home matches, in the bare form
// ps prints and the quoted form Windows builds.
func TestStartedWithHome(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		homeDir string
		want    bool
	}{
		{
			name:    "macOS ps",
			line:    "/Applications/Hop.app/Contents/Resources/bin/mihomo -d /Library/Application Support/io.github.marukolibro.hop.helper -f /Library/Application Support/io.github.marukolibro.hop.helper/config.yaml",
			homeDir: "/Library/Application Support/io.github.marukolibro.hop.helper",
			want:    true,
		},
		{
			name:    "windows quoted",
			line:    `"C:\Program Files\Hop\helper\bin\mihomo.exe" -d "C:\Program Files\Hop\helper\home" -f "C:\Program Files\Hop\helper\home\config.yaml"`,
			homeDir: `C:\Program Files\Hop\helper\home`,
			want:    true,
		},
		{
			name:    "windows unquoted",
			line:    `C:\Hop\bin\mihomo.exe -d C:\Hop\home -f C:\Hop\home\config.yaml`,
			homeDir: `C:\Hop\home`,
			want:    true,
		},
		{
			name:    "another home with the same prefix",
			line:    `mihomo -d C:\Hop\home2 -f C:\Hop\home2\config.yaml`,
			homeDir: `C:\Hop\home`,
			want:    false,
		},
		{
			name:    "the home only appears elsewhere",
			line:    `mihomo -d /tmp/other -f /tmp/other/config.yaml --log /tmp/home`,
			homeDir: "/tmp/home",
			want:    false,
		},
		{name: "no process", line: "", homeDir: "/tmp/home", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := startedWithHome(tc.line, tc.homeDir); got != tc.want {
				t.Fatalf("startedWithHome(%q, %q) = %v, want %v", tc.line, tc.homeDir, got, tc.want)
			}
		})
	}
}
