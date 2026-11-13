package wsl

import (
	"strings"
	"testing"
)

func TestBuildLaunchScript_DoesNotSingleQuoteWholeExecCommand(t *testing.T) {
	env := map[string]string{
		"OPENAI_API_BASE_URL": "http://172.29.16.1:1234/v1",
	}
	cmd := []string{"bash", "-c", "exec -a 'lapel-test' sleep 30"}
	script := buildLaunchScript("/tmp/lapel-123.pg", "/home/agent", env, cmd)

	if !strings.Contains(script, "echo $$ > '/tmp/lapel-123.pg'\n") {
		t.Errorf("missing pid recording line in script:\n%s", script)
	}
	if !strings.Contains(script, "export 'OPENAI_API_BASE_URL'='http://172.29.16.1:1234/v1'\n") {
		t.Errorf("missing export line in script:\n%s", script)
	}
	if !strings.Contains(script, "cd '/home/agent' || exit 1\n") {
		t.Errorf("missing cd line in script:\n%s", script)
	}
	wantExec := "exec 'bash' '-c' 'exec -a '\\''lapel-test'\\'' sleep 30'\n"
	if !strings.Contains(script, wantExec) {
		t.Errorf("unexpected exec line:\ngot:\n%s\nwant substring:\n%s", script, wantExec)
	}
}
