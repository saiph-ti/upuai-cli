package git

import "testing"

func TestParseRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"canonical owner/repo", "gbmiranda/geniapost", "gbmiranda/geniapost", false},
		{"https url", "https://github.com/gbmiranda/geniapost", "gbmiranda/geniapost", false},
		{"https url with .git", "https://github.com/gbmiranda/geniapost.git", "gbmiranda/geniapost", false},
		{"http url", "http://github.com/gbmiranda/geniapost", "gbmiranda/geniapost", false},
		{"ssh url", "git@github.com:gbmiranda/geniapost.git", "gbmiranda/geniapost", false},
		{"ssh url no .git", "git@github.com:gbmiranda/geniapost", "gbmiranda/geniapost", false},
		{"hostless", "github.com/gbmiranda/geniapost", "gbmiranda/geniapost", false},
		{"gitlab host stripped", "gitlab.com/group/proj", "group/proj", false},
		{"trailing slash", "gbmiranda/geniapost/", "gbmiranda/geniapost", false},
		{"trailing slash on url", "https://github.com/gbmiranda/geniapost/", "gbmiranda/geniapost", false},
		{"with whitespace", "  gbmiranda/geniapost  ", "gbmiranda/geniapost", false},
		{"dots and dashes valid", "my.org/my-repo.v2", "my.org/my-repo.v2", false},

		{"empty", "", "", true},
		{"only owner", "gbmiranda", "", true},
		{"three segments", "gbmiranda/geniapost/sub", "", true},
		{"three segments via url", "https://github.com/o/r/sub", "", true},
		{"auth embedded https", "https://user:token@github.com/o/r", "", true},
		{"auth embedded http", "http://user:tok@github.com/o/r", "", true},
		{"invalid chars", "gbmiranda/genia post", "", true},
		{"missing repo", "owner/", "", true},
		{"missing owner", "/repo", "", true},
		{"ssh missing colon", "git@github.com/o/r", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepo(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRepo(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseRepo(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeRootDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{".", ""},
		{"/", ""},
		{"./", ""},
		{"  ", ""},
		{" . ", ""},
		{"apps/api", "apps/api"},
		{"./apps/api", "apps/api"},
		{"apps/api/", "apps/api/"},
		{"sub", "sub"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormalizeRootDir(tt.in); got != tt.want {
				t.Fatalf("NormalizeRootDir(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
