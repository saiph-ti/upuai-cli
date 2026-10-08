package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/upuai-cloud/cli/internal/config"
)

func publicAccessClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")
	return NewClient()
}

// API anterior ao MySQL público não manda `engine`: é Postgres, e os campos
// novos ficam zerados — GET e PUT normalizam igual.
func TestPublicAccessEmptyEngineIsPostgres(t *testing.T) {
	c := publicAccessClient(t, `{"enabled":true,"host":"abc.db.upuai.cloud","port":5432,"connectionString":"postgresql://u:p@abc.db.upuai.cloud:5432/app?sslmode=verify-full"}`)
	for name, call := range map[string]func() (*PublicAccessInfo, error){
		"GET": func() (*PublicAccessInfo, error) { return c.GetDatabasePublicAccess("env", "svc") },
		"PUT": func() (*PublicAccessInfo, error) { return c.SetDatabasePublicAccess("env", "svc", true, nil) },
	} {
		info, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Engine != DatabaseEnginePostgres || info.IsMySQL() {
			t.Fatalf("%s: engine = %q, quero %q", name, info.Engine, DatabaseEnginePostgres)
		}
		if info.Host != "abc.db.upuai.cloud" || info.Port != 5432 {
			t.Fatalf("%s: %+v", name, info)
		}
	}
}

func TestPublicAccessMySQLFields(t *testing.T) {
	c := publicAccessClient(t, `{"engine":"mysql","enabled":true,"host":"orders.db.upuai.cloud","port":23307,
		"connectionString":"mysql://app:s3cret@orders.db.upuai.cloud:23307/app?ssl-mode=VERIFY_IDENTITY",
		"allowedCidrs":["203.0.113.7/32"],"username":"app","password":"s3cret","database":"app","serverTlsReady":true}`)
	info, err := c.GetDatabasePublicAccess("env", "svc")
	if err != nil {
		t.Fatal(err)
	}
	want := PublicAccessInfo{
		Engine: "mysql", Enabled: true, Host: "orders.db.upuai.cloud", Port: 23307,
		ConnectionString: "mysql://app:s3cret@orders.db.upuai.cloud:23307/app?ssl-mode=VERIFY_IDENTITY",
		Username:         "app", Password: "s3cret", Database: "app", ServerTLSReady: true,
	}
	if !info.IsMySQL() || info.Engine != want.Engine || info.Host != want.Host || info.Port != want.Port ||
		info.ConnectionString != want.ConnectionString || info.Username != want.Username ||
		info.Password != want.Password || info.Database != want.Database || !info.ServerTLSReady ||
		len(info.AllowedCidrs) != 1 || info.AllowedCidrs[0] != "203.0.113.7/32" {
		t.Fatalf("decode = %+v", info)
	}
}

// Os códigos novos do endpoint público chegam em `error` (envelope de cluster)
// e precisam virar ErrorCode — é por ele que a CLI traduz a mensagem.
func TestPublicAccessClusterErrorCodes(t *testing.T) {
	c := &Client{}
	for _, tc := range []struct {
		status int
		code   string
	}{
		{409, "PUBLIC_ACCESS_UNSUPPORTED_ENGINE"},
		{409, "PUBLIC_ACCESS_UNAVAILABLE"},
		{409, "PUBLIC_PORT_POOL_EXHAUSTED"},
		{409, "PUBLIC_PORT_CONFLICT"},
		{500, "PUBLIC_PORT_INVALID"},
	} {
		err := c.parseError(respWith(tc.status, `{"statusCode":409,"error":"`+tc.code+`","message":"x","details":{"actionable":false,"retryable":false}}`))
		if got := ErrorCode(err); got != tc.code {
			t.Errorf("ErrorCode = %q, quero %q", got, tc.code)
		}
	}
}
