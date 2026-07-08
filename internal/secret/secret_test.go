package secret

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Fixture secrets are assembled at runtime so this repository never contains
// anything that looks like a credential to other scanners (including us).

func fakeAWSKey() string { return "AKIA" + "QQQQQQQQQQQQQQQQ" }

func fakePrivateKey() string {
	return "-----BEGIN RSA " + "PRIVATE KEY-----\nQQQQQQQQQQ\n-----END RSA " + "PRIVATE KEY-----"
}

func fakeHighEntropy() string {
	return "u9Xk2Lp8" + "Qw4Rz7Tv" + "1Mn5Bc3J" + "h6Fd0Gs2"
}

func mustLoad(t *testing.T) *Scanner {
	t.Helper()
	s, err := Load()
	require.NoError(t, err)
	return s
}

func TestLoadCompilesAllRules(t *testing.T) {
	s := mustLoad(t)
	require.NotEmpty(t, s.rules)
	require.NotEmpty(t, s.allow)
}

func TestScanAWSAccessKey(t *testing.T) {
	s := mustLoad(t)
	content := "cfg := aws.Config{Key: \"" + fakeAWSKey() + "\"}\n"
	findings := s.Scan("cfg.go", content2bytes(content))
	require.NotEmpty(t, findings)
	require.Equal(t, "aws-access-key-id", findings[0].RuleID)
	require.Equal(t, 1, findings[0].Line)
	require.NotContains(t, findings[0].Preview, fakeAWSKey(), "preview never exposes the full secret")
}

func TestScanPrivateKeyBlockSpansLines(t *testing.T) {
	s := mustLoad(t)
	content := "header\n" + fakePrivateKey() + "\nfooter\n"
	findings := s.Scan("id_rsa", content2bytes(content))
	require.Len(t, findings, 1)
	require.Equal(t, "private-key", findings[0].RuleID)
	require.Equal(t, 2, findings[0].Line)

	redacted, n := Redact(content2bytes(content), findings)
	require.Equal(t, 1, n)
	require.NotContains(t, string(redacted), "QQQQQQQQQQ", "whole block is redacted, not just the BEGIN line")
	require.Contains(t, string(redacted), "[REDACTED:private-key]")
	require.Contains(t, string(redacted), "header\n")
	require.Contains(t, string(redacted), "\nfooter\n")
}

func TestScanGenericHighEntropy(t *testing.T) {
	s := mustLoad(t)
	content := "api_key = \"" + fakeHighEntropy() + "\"\n"
	findings := s.Scan(".env.dev", content2bytes(content))
	require.NotEmpty(t, findings)
	require.Equal(t, "generic-api-key-quoted", findings[0].RuleID)
}

func TestScanLowEntropyValueIgnored(t *testing.T) {
	s := mustLoad(t)
	content := "api_key = \"aaaaaaaaaaaaaaaaaaaa\"\n" // 20 chars, entropy ~0
	require.Empty(t, s.Scan("cfg.py", content2bytes(content)))
}

func fakeDBPassword() string { return "s3cr3t" + "P4ssw0rdX" }

func TestScanDatabaseURLPassword(t *testing.T) {
	s := mustLoad(t)
	content := "DATABASE_URL=postgres:" + "//admin:" + fakeDBPassword() + "@db.local:5432/app\n"
	findings := s.Scan(".envrc", content2bytes(content))
	found := false
	for _, f := range findings {
		if f.RuleID == "database-url-password" {
			found = true
		}
	}
	require.True(t, found, "findings: %v", findings)

	redacted, _ := Redact(content2bytes(content), findings)
	require.NotContains(t, string(redacted), fakeDBPassword())
	require.Contains(t, string(redacted), "//admin:", "only the password is replaced")
}

func TestAllowlistPlaceholders(t *testing.T) {
	s := mustLoad(t)
	for _, line := range []string{
		"api_key = \"your-api-key-goes-here-ok\"\n",
		"token = \"${GITHUB_TOKEN_FROM_ENV}\"\n",
		"password = \"CHANGEME_CHANGEME_1\"\n",
	} {
		require.Empty(t, s.Scan("docs.md", content2bytes(line)), "line %q should be allowlisted", line)
	}
}

func TestSuppressionComment(t *testing.T) {
	s := mustLoad(t)
	content := "key := \"" + fakeAWSKey() + "\" // " + AllowMarker + ": doc fixture\n"
	require.Empty(t, s.Scan("cfg.go", content2bytes(content)))
}

func TestCleanContent(t *testing.T) {
	s := mustLoad(t)
	content := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n"
	require.Empty(t, s.Scan("main.go", content2bytes(content)))
}

func TestRedactMergesOverlaps(t *testing.T) {
	content := content2bytes("abcdef")
	findings := []Finding{
		{RuleID: "r1", Start: 1, End: 4},
		{RuleID: "r2", Start: 3, End: 5},
	}
	redacted, n := Redact(content, findings)
	require.Equal(t, 1, n)
	require.Equal(t, "a[REDACTED:r1]f", string(redacted))
}

func TestShannonEntropy(t *testing.T) {
	require.Equal(t, 0.0, shannonEntropy([]byte("aaaa")))
	require.Greater(t, shannonEntropy([]byte(fakeHighEntropy())), 3.5)
}

func content2bytes(s string) []byte { return []byte(s) }
