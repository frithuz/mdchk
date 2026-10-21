package config

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/strictjson"
)

// DecodeJSON validates raw supplied configuration without filesystem lookup.
// Strict raw validation precedes the same schema/domain pipeline used by YAML.
// Origins carry property paths, without invented file paths or text positions.
func DecodeJSON(content []byte) (*Definition, error) {
	raw, err := strictjson.Decode(content, configBytes)
	if err != nil {
		var e *strictjson.Error
		if errors.As(err, &e) {
			return nil, &DecodeError{Message: e.Message, Origin: Origin{PropertyPath: e.Path}}
		}
		return nil, err
	}
	root, err := jsonValue(raw, "")
	if err != nil {
		return nil, err
	}
	return definition(root)
}
func jsonValue(raw strictjson.Value, path string) (LocatedValue, error) {
	n := &syntaxValue{origin: Origin{PropertyPath: path}}
	switch raw.Kind {
	case "object":
		n.kind = MappingValue
		for _, e := range raw.Object {
			v, err := jsonValue(e.Value, childPath(path, e.Name))
			if err != nil {
				return LocatedValue{}, err
			}
			n.entries = append(n.entries, LocatedEntry{Key: e.Name, Origin: v.Origin(), Value: v})
		}
	case "array":
		n.kind = SequenceValue
		for i, e := range raw.Array {
			v, err := jsonValue(e, path+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return LocatedValue{}, err
			}
			n.elements = append(n.elements, v)
		}
	case "string":
		n.kind = StringValue
		n.text = raw.Scalar.(string)
	case "boolean":
		n.kind = BooleanValue
		n.text = strconv.FormatBool(raw.Scalar.(bool))
	case "number":
		n.text = string(raw.Scalar.(json.Number))
		n.kind = NumberValue
		if !strings.ContainsAny(n.text, ".eE") {
			n.kind = IntegerValue
			x, e := strconv.Atoi(n.text)
			n.conversion = &scalarConversion{value: x, representable: e == nil}
		} else {
			x, e := strconv.ParseFloat(n.text, 64)
			n.conversion = &scalarConversion{value: x, representable: e == nil}
		}
	default:
		return LocatedValue{}, &DecodeError{Message: "null settings are not permitted", Origin: Origin{PropertyPath: path}}
	}
	return LocatedValue{node: n}, nil
}
