package conformance_test

import "testing"

// Helpers for reading an advertised tool schema. Kept separate so the
// assertions above read as contract statements rather than as map plumbing.

func outputSchema(t *testing.T, tool map[string]interface{}, name string) map[string]interface{} {
	t.Helper()
	schema, ok := tool["outputSchema"].(map[string]interface{})
	if !ok {
		t.Errorf("%s declares no outputSchema", name)
		return nil
	}
	return schema
}

func outputProperties(t *testing.T, tool map[string]interface{}, name string) map[string]interface{} {
	t.Helper()
	schema := outputSchema(t, tool, name)
	if schema == nil {
		return map[string]interface{}{}
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Errorf("%s outputSchema declares no properties", name)
		return map[string]interface{}{}
	}
	return props
}

func requiredNames(t *testing.T, tool map[string]interface{}, name string) []string {
	t.Helper()
	schema := outputSchema(t, tool, name)
	if schema == nil {
		return nil
	}
	listed, ok := schema["required"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(listed))
	for _, entry := range listed {
		if field, ok := entry.(string); ok {
			out = append(out, field)
		}
	}
	return out
}

// assertEnum holds a field's vocabulary to exactly the spec's values, in any
// order. Exactly, not "contains": an extra value is a server inventing
// vocabulary a client has no rule for, and a missing one is a client coding
// against a value it will never see.
func assertEnum(t *testing.T, label string, field map[string]interface{}, want ...string) {
	t.Helper()
	listed, ok := field["enum"].([]interface{})
	if !ok {
		t.Errorf("%s declares no enum, so its vocabulary is not closed", label)
		return
	}
	got := make([]string, 0, len(listed))
	for _, entry := range listed {
		if value, ok := entry.(string); ok {
			got = append(got, value)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s enum = %v, spec says %v", label, got, want)
		return
	}
	for _, value := range want {
		if !contains(got, value) {
			t.Errorf("%s enum = %v, missing %q", label, got, value)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, entry := range haystack {
		if entry == needle {
			return true
		}
	}
	return false
}
