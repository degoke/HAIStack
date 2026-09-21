package cql

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func parseELMLibrary(data []byte) (*Library, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, errf("%w: ELM library is empty", ErrUnsupported)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, errf("decode ELM library: %w", err)
	}
	obj, ok := asObject(root)
	if !ok {
		return nil, errf("%w: ELM library must be a JSON object", ErrUnsupported)
	}
	libObj := obj
	if inner, ok := asObject(obj["library"]); ok {
		libObj = inner
	}
	lib := &Library{Source: string(data), Context: "Patient"}
	if ident, ok := asObject(libObj["identifier"]); ok {
		if id := elmString(ident["id"]); id != "" {
			lib.Name = id
		}
		if v := elmString(ident["version"]); v != "" {
			lib.Version = v
		}
	}
	if lib.Name == "" {
		lib.Name = elmString(libObj["localId"])
	}
	for _, u := range elmDefs(libObj["usings"]) {
		local := elmString(u["localIdentifier"])
		if local != "" && !strings.EqualFold(local, "System") {
			lib.Using = local
			break
		}
	}
	for _, inc := range elmDefs(libObj["includes"]) {
		name := firstNonEmpty(elmString(inc["path"]), elmString(inc["localIdentifier"]))
		if name == "" {
			continue
		}
		called := elmString(inc["localIdentifier"])
		if called == name {
			called = ""
		}
		lib.Includes = append(lib.Includes, Include{
			Name:    name,
			Version: elmString(inc["version"]),
			Called:  called,
		})
	}
	for _, cs := range elmDefs(libObj["codeSystems"]) {
		name := elmString(cs["name"])
		if name == "" {
			continue
		}
		lib.CodeSystems = append(lib.CodeSystems, CodeSystem{Name: name, URL: elmString(cs["id"])})
	}
	for _, vs := range elmDefs(libObj["valueSets"]) {
		name := elmString(vs["name"])
		if name == "" {
			continue
		}
		lib.ValueSets = append(lib.ValueSets, ValueSet{Name: name, URL: elmString(vs["id"])})
	}
	for _, c := range elmDefs(libObj["codes"]) {
		name := elmString(c["name"])
		if name == "" {
			continue
		}
		system := elmString(c["codeSystem"])
		if cs, ok := asObject(c["codeSystem"]); ok {
			system = firstNonEmpty(elmString(cs["name"]), elmString(cs["id"]))
		}
		lib.Codes = append(lib.Codes, Code{
			Name:    name,
			Code:    firstNonEmpty(elmString(c["id"]), elmString(c["code"])),
			System:  system,
			Display: elmString(c["display"]),
		})
	}
	for _, c := range elmDefs(libObj["concepts"]) {
		name := elmString(c["name"])
		if name == "" {
			continue
		}
		var codes []string
		for _, ref := range elmList(c["code"]) {
			if n := elmString(ref["name"]); n != "" {
				codes = append(codes, n)
			}
		}
		lib.Concepts = append(lib.Concepts, Concept{Name: name, Codes: codes, Display: elmString(c["display"])})
	}
	for _, p := range elmDefs(libObj["parameters"]) {
		name := elmString(p["name"])
		if name == "" {
			continue
		}
		param := Parameter{Name: name, Type: elmTypeName(p["parameterTypeSpecifier"])}
		if def, ok := asObject(p["default"]); ok {
			n, err := parseELMExpr(def)
			if err != nil {
				return nil, err
			}
			param.Default = n
		}
		lib.Parameters = append(lib.Parameters, param)
	}
	contextFromDecl := false
	for _, ctx := range elmDefs(libObj["contexts"]) {
		if name := elmString(ctx["name"]); name != "" {
			lib.Context = name
			contextFromDecl = true
		}
	}
	stmts := elmDefs(libObj["statements"])
	for _, def := range stmts {
		if err := appendELMStatement(lib, def); err != nil {
			return nil, err
		}
	}
	if !contextFromDecl {
		lib.Context = elmLibraryContext(stmts)
	}
	if len(lib.Defines) == 0 && len(lib.Functions) == 0 {
		return nil, errf("%w: ELM library has no statements", ErrUnsupported)
	}
	resolveTerminologyDecls(lib)
	return lib, nil
}

func elmLibraryContext(stmts []map[string]any) string {
	last := "Patient"
	sawPatient := false
	for _, def := range stmts {
		ctx := elmString(def["context"])
		if ctx == "" {
			continue
		}
		last = ctx
		if strings.EqualFold(ctx, "Patient") {
			sawPatient = true
		}
	}
	if sawPatient {
		return "Patient"
	}
	return last
}

func appendELMStatement(lib *Library, def map[string]any) error {
	name := elmString(def["name"])
	if name == "" {
		return nil
	}
	if isELMContextPatientDef(def) {
		return nil
	}
	access := strings.ToLower(elmString(def["accessLevel"]))
	if access == "" {
		access = "public"
	}
	if isELMFunctionDef(def) {
		fn, err := parseELMFunction(def, name, access)
		if err != nil {
			return err
		}
		lib.Functions = append(lib.Functions, fn)
		return nil
	}
	exprObj, ok := asObject(def["expression"])
	if !ok {
		return errf("%w: ELM statement %q has no expression", ErrUnsupported, name)
	}
	expr, err := parseELMExpr(exprObj)
	if err != nil {
		return err
	}
	lib.Defines = append(lib.Defines, Define{Name: name, Access: access, Expression: expr, Source: name})
	return nil
}

func isELMContextPatientDef(def map[string]any) bool {
	if !strings.EqualFold(elmString(def["name"]), "Patient") {
		return false
	}
	expr, ok := asObject(def["expression"])
	if !ok {
		return false
	}
	if strings.EqualFold(elmType(expr), "SingletonFrom") {
		ops := elmList(expr["operand"])
		if len(ops) == 0 {
			if inner, ok := asObject(expr["operand"]); ok {
				ops = []map[string]any{inner}
			}
		}
		if len(ops) != 1 {
			return false
		}
		expr = ops[0]
	}
	if !strings.EqualFold(elmType(expr), "Retrieve") {
		return false
	}
	rt := elmTypeBare(firstNonEmpty(elmString(expr["dataType"]), elmString(expr["templateId"])))
	return strings.EqualFold(rt, "Patient")
}

func isELMFunctionDef(def map[string]any) bool {
	if strings.EqualFold(elmType(def), "FunctionDef") {
		return true
	}
	ops := elmList(def["operand"])
	if len(ops) == 0 {
		return false
	}
	for _, op := range ops {
		if elmType(op) != "" {
			return false
		}
		if elmString(op["name"]) == "" {
			return false
		}
	}
	_, hasExpr := asObject(def["expression"])
	return hasExpr
}

func parseELMFunction(def map[string]any, name, access string) (Function, error) {
	fn := Function{Name: name, Access: access, Source: name}
	if strings.EqualFold(elmString(def["fluent"]), "true") || elmBool(def["fluent"]) {
		fn.Fluent = true
	}
	for _, op := range elmList(def["operand"]) {
		pname := elmString(op["name"])
		if pname == "" {
			continue
		}
		fn.Params = append(fn.Params, FunctionParam{Name: pname, Type: elmTypeName(op["operandTypeSpecifier"])})
	}
	body, ok := asObject(def["expression"])
	if !ok {
		return Function{}, errf("%w: ELM function %q has no body", ErrUnsupported, name)
	}
	expr, err := parseELMExpr(body)
	if err != nil {
		return Function{}, err
	}
	fn.Body = expr
	return fn, nil
}

func recoverCQLFromELM(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		if looksLikeCQL(string(raw)) {
			return strings.TrimSpace(string(raw))
		}
		return ""
	}
	obj, ok := asObject(v)
	if !ok {
		return ""
	}
	libObj := obj
	if inner, ok := asObject(obj["library"]); ok {
		libObj = inner
	}
	var found []string
	walkCQLStrings(libObj["cql"], &found)
	walkCQLStrings(libObj["annotation"], &found)
	best := ""
	for _, s := range found {
		if len(s) > len(best) {
			best = s
		}
	}
	if looksLikeCQL(best) {
		return strings.TrimSpace(best)
	}
	if src := flattenLibraryAnnotation(v); looksLikeCQL(src) {
		return strings.TrimSpace(src)
	}
	return ""
}

func walkCQLStrings(v any, found *[]string) {
	switch x := v.(type) {
	case string:
		if looksLikeCQL(x) {
			*found = append(*found, x)
		}
	case []any:
		for _, el := range x {
			walkCQLStrings(el, found)
		}
	case map[string]any:
		for _, el := range x {
			walkCQLStrings(el, found)
		}
	}
}

func flattenLibraryAnnotation(v any) string {
	obj, ok := asObject(v)
	if !ok {
		return ""
	}
	libObj := obj
	if inner, ok := asObject(obj["library"]); ok {
		libObj = inner
	}
	var b strings.Builder
	collectELMAnnotation(libObj["annotation"], &b, false)
	return b.String()
}

func collectELMAnnotation(v any, b *strings.Builder, inAnn bool) {
	switch x := v.(type) {
	case []any:
		for _, el := range x {
			collectELMAnnotation(el, b, inAnn)
		}
	case map[string]any:
		if t := elmString(x["type"]); strings.EqualFold(t, "Annotation") {
			inAnn = true
		}
		if !inAnn {
			return
		}
		if vals, ok := x["value"].([]any); ok {
			for _, v := range vals {
				if s, ok := v.(string); ok {
					b.WriteString(s)
				}
			}
		}
		if s, ok := x["s"]; ok {
			collectELMAnnotation(s, b, true)
		}
	}
}

func looksLikeCQL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "library") {
		return false
	}
	rest := strings.TrimSpace(lower[len("library"):])
	if rest == "" {
		return false
	}
	return strings.Contains(lower, "define ") || strings.Contains(lower, "define\t") || strings.Contains(lower, "define\n")
}

func elmDefs(v any) []map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := asObject(v); ok {
		if def, ok := m["def"]; ok {
			return elmDefs(def)
		}
		return []map[string]any{m}
	}
	return elmList(v)
}

func elmList(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return []map[string]any{x}
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, el := range x {
			if m, ok := asObject(el); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func elmType(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	return elmString(obj["type"])
}

func elmTypeName(v any) string {
	if s, ok := v.(string); ok {
		return elmTypeBare(s)
	}
	if m, ok := asObject(v); ok {
		return firstNonEmpty(elmTypeBare(elmString(m["name"])), elmTypeBare(elmString(m["resultTypeName"])), elmTypeName(m["elementType"]), elmTypeName(m["pointType"]))
	}
	return ""
}

func elmTypeBare(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "}"); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	return s
}

func elmString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case map[string]any:
		return firstNonEmpty(elmString(x["id"]), elmString(x["name"]), elmString(x["value"]))
	default:
		return ""
	}
}

func elmBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		b, _ := parseBoolString(x)
		return b
	case float64:
		return x != 0
	}
	return false
}

func elmInt(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), x == float64(int64(x))
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case int:
		return int64(x), true
	case int64:
		return x, true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	}
	return 0, false
}

func elmNumber(v any) any {
	if n, ok := elmInt(v); ok {
		return n
	}
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return f
		}
	}
	return float64(0)
}

func parseBoolString(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func parseELMDateString(s string, dateTime bool) (time.Time, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "@"))
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999", "2006-01-02T15:04:05", "2006-01-02"}
	if !dateTime {
		layouts = []string{"2006-01-02", time.RFC3339}
	}
	for _, layout := range layouts {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseELMTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "@T"))
	s = strings.TrimPrefix(s, "T")
	for _, layout := range []string{"15:04:05.999", "15:04:05", "15:04"} {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm.UTC(), true
		}
	}
	return time.Time{}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
