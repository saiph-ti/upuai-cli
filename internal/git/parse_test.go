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

func TestParseRepoWithProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		wantRepo     string
		wantProvider string
		wantErr      bool
	}{
		{"github https", "https://github.com/o/r", "o/r", "github", false},
		{"github www", "https://www.github.com/o/r", "o/r", "github", false},
		{"gitlab https", "https://gitlab.com/group/proj.git", "group/proj", "gitlab", false},
		{"gitlab ssh", "git@gitlab.com:group/proj.git", "group/proj", "gitlab", false},
		{"gitlab hostless", "gitlab.com/group/proj", "group/proj", "gitlab", false},
		{"shorthand no host", "o/r", "o/r", "", false},
		{"bitbucket unsupported", "https://bitbucket.org/o/r", "o/r", "bitbucket", false},
		{"self-hosted explicit host", "https://git.acme.com/o/r", "o/r", "git.acme.com", false},
		{"auth embedded", "https://user:tok@gitlab.com/o/r", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, provider, err := ParseRepoWithProvider(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRepoWithProvider(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if repo != tt.wantRepo || provider != tt.wantProvider {
				t.Fatalf("ParseRepoWithProvider(%q) = (%q, %q), want (%q, %q)", tt.in, repo, provider, tt.wantRepo, tt.wantProvider)
			}
		})
	}
}

func TestResolveProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		explicit string
		detected string
		want     string
		wantErr  bool
	}{
		{"detected gitlab", "", "gitlab", "gitlab", false},
		{"detected github", "", "github", "github", false},
		{"shorthand defaults github", "", "", "github", false},
		{"explicit gitlab respected", "gitlab", "", "gitlab", false},
		{"explicit matches detected", "gitlab", "gitlab", "gitlab", false},
		{"explicit conflicts host", "github", "gitlab", "", true},
		{"unsupported host rejected", "", "bitbucket", "", true},
		{"self-hosted rejected", "", "git.acme.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProvider(tt.explicit, tt.detected)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveProvider(%q,%q) err = %v, wantErr %v", tt.explicit, tt.detected, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ResolveProvider(%q,%q) = %q, want %q", tt.explicit, tt.detected, got, tt.want)
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
