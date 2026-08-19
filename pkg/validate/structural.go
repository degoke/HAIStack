package validate

import "strings"

// structuralDiagnostics formats proto/jsonformat structural errors for API consumers.
func structuralDiagnostics(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	const prefix = `error at "`
	if !strings.HasPrefix(msg, prefix) {
		return msg
	}
	rest := msg[len(prefix):]
	sep := `": `
	if idx := strings.Index(rest, sep); idx >= 0 {
		return rest[:idx] + ": " + rest[idx+len(sep):]
	}
	return msg
}
