package macnotify

import (
	"howett.net/plist"
)

func decodeRequest(data []byte) (title, subtitle, body, app string) {
	var top map[string]any
	if _, err := plist.Unmarshal(data, &top); err != nil || top == nil {
		return
	}
	app = asString(top["app"])
	if req, ok := asMap(top["req"]); ok {
		title = asString(req["titl"])
		subtitle = asString(req["subt"])
		body = asString(req["body"])
	}
	if title == "" && subtitle == "" && body == "" {
		title, subtitle, body, app = decodeKeyed(top, app)
	}
	return
}

func decodeKeyed(top map[string]any, app string) (title, subtitle, body, outApp string) {
	outApp = app
	objects, ok := top["$objects"].([]any)
	if !ok {
		return
	}
	root := keyedMap(objects, top["$top"])
	if req := keyedMap(objects, root["req"]); req != nil {
		title = keyedString(objects, req["titl"])
		subtitle = keyedString(objects, req["subt"])
		body = keyedString(objects, req["body"])
	}
	if title == "" {
		title = keyedString(objects, root["NSTitle"])
		subtitle = keyedString(objects, root["NSSubtitle"])
		body = keyedString(objects, root["NSInformativetext"])
	}
	if outApp == "" {
		outApp = keyedString(objects, root["app"])
	}
	return
}

func keyedMap(objects []any, v any) map[string]any {
	v = deref(objects, v)
	if m, ok := asMap(v); ok {
		return m
	}
	return nil
}

func keyedString(objects []any, v any) string {
	return asString(deref(objects, v))
}

func deref(objects []any, v any) any {
	switch t := v.(type) {
	case plist.UID:
		i := int(t)
		if i >= 0 && i < len(objects) {
			return objects[i]
		}
	case uint64:
		i := int(t)
		if i >= 0 && i < len(objects) {
			return objects[i]
		}
	case map[string]any:
		if inner, ok := t["UID"]; ok {
			return deref(objects, inner)
		}
	}
	return v
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case map[string]any:
		if s, ok := t["NS.string"].(string); ok {
			return s
		}
		return ""
	default:
		return ""
	}
}
