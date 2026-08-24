package gate

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

// Schema is a deliberately small, auditable subset of JSON Schema. Object
// schemas are always closed: keys not listed in Properties are rejected.
type Schema struct {
	Type       string             `json:"type"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	Enum       []any              `json:"enum,omitempty"`
	Pattern    string             `json:"pattern,omitempty"`
	MinLength  *int               `json:"minLength,omitempty"`
	MaxLength  *int               `json:"maxLength,omitempty"`
	Minimum    *float64           `json:"minimum,omitempty"`
	Maximum    *float64           `json:"maximum,omitempty"`
	MinItems   *int               `json:"minItems,omitempty"`
	MaxItems   *int               `json:"maxItems,omitempty"`
}

func (s *Schema) Validate(v any) error { return s.validate(v, "$") }

func (s *Schema) validate(v any, path string) error {
	if s == nil {
		return fmt.Errorf("%s: schema is missing", path)
	}
	if len(s.Enum) > 0 {
		matched := false
		for _, allowed := range s.Enum {
			if equalJSON(v, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value is not in enum", path)
		}
	}
	switch s.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		for _, key := range s.Required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("%s.%s: required property is missing", path, key)
			}
		}
		for key, value := range obj {
			child, ok := s.Properties[key]
			if !ok {
				return fmt.Errorf("%s.%s: additional properties are forbidden", path, key)
			}
			if err := child.validate(value, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		if s.MinItems != nil && len(arr) < *s.MinItems {
			return fmt.Errorf("%s: has fewer than %d items", path, *s.MinItems)
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			return fmt.Errorf("%s: has more than %d items", path, *s.MaxItems)
		}
		for i, item := range arr {
			if err := s.Items.validate(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: expected string", path)
		}
		if s.MinLength != nil && len([]rune(str)) < *s.MinLength {
			return fmt.Errorf("%s: string is too short", path)
		}
		if s.MaxLength != nil && len([]rune(str)) > *s.MaxLength {
			return fmt.Errorf("%s: string is too long", path)
		}
		if s.Pattern != "" {
			re, err := regexp.Compile(s.Pattern)
			if err != nil {
				return fmt.Errorf("%s: invalid schema pattern", path)
			}
			if !re.MatchString(str) {
				return fmt.Errorf("%s: string does not match pattern", path)
			}
		}
	case "number", "integer":
		n, ok := v.(float64)
		if !ok || (s.Type == "integer" && math.Trunc(n) != n) {
			return fmt.Errorf("%s: expected %s", path, s.Type)
		}
		if s.Minimum != nil && n < *s.Minimum {
			return fmt.Errorf("%s: number is below minimum", path)
		}
		if s.Maximum != nil && n > *s.Maximum {
			return fmt.Errorf("%s: number is above maximum", path)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	case "null":
		if v != nil {
			return fmt.Errorf("%s: expected null", path)
		}
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, s.Type)
	}
	return nil
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
