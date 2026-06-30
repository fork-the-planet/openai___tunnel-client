package wiretypes

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAPIContractSurface(t *testing.T) {
	spec, raw := readOpenAPISpec(t)

	if got := mustString(t, spec["openapi"], "openapi"); got != "3.1.0" {
		t.Fatalf("expected OpenAPI 3.1.0, got %q", got)
	}
	if strings.Contains(string(raw), "x-openai-") {
		t.Fatal("public OpenAPI contract must not contain service-internal extensions")
	}

	paths := mustMap(t, spec["paths"], "paths")
	expected := map[string]string{
		"/v1/tunnels/{tunnel_id}":          "get",
		"/v1/tunnels/{tunnel_id}/poll":     "get",
		"/v1/tunnels/{tunnel_id}/response": "post",
	}
	if len(paths) != len(expected) {
		t.Fatalf("expected only %d public paths, got %d", len(expected), len(paths))
	}
	for path, method := range expected {
		pathItem := mustMap(t, paths[path], "paths."+path)
		if len(pathItem) != 1 {
			t.Fatalf("expected exactly one method for %s, got %v", path, mapKeys(pathItem))
		}
		if _, ok := pathItem[method]; !ok {
			t.Fatalf("expected %s %s, got methods %v", strings.ToUpper(method), path, mapKeys(pathItem))
		}
	}

	assertOperationID(t, spec, "/v1/tunnels/{tunnel_id}", "get", "getTunnelMetadata")
	assertOperationID(t, spec, "/v1/tunnels/{tunnel_id}/poll", "get", "pollTunnelCommands")
	assertOperationID(t, spec, "/v1/tunnels/{tunnel_id}/response", "post", "postTunnelResponse")

	poll := operation(t, spec, "/v1/tunnels/{tunnel_id}/poll", "get")
	responses := mustMap(t, poll["responses"], "poll.responses")
	if _, ok := responses["204"]; !ok {
		t.Fatal("poll operation must document 204 No Content")
	}

	response := operation(t, spec, "/v1/tunnels/{tunnel_id}/response", "post")
	if !hasRequiredHeader(response, "X-Tunnel-Shard-Token") {
		t.Fatal("response operation must require X-Tunnel-Shard-Token")
	}

	components := mustMap(t, spec["components"], "components")
	securitySchemes := mustMap(t, components["securitySchemes"], "components.securitySchemes")
	bearer := mustMap(t, securitySchemes["BearerAuth"], "components.securitySchemes.BearerAuth")
	if got := mustString(t, bearer["scheme"], "BearerAuth.scheme"); got != "bearer" {
		t.Fatalf("expected bearer auth scheme, got %q", got)
	}
}

func TestOpenAPIExamplesMatchWireTypes(t *testing.T) {
	spec, _ := readOpenAPISpec(t)

	poll := operation(t, spec, "/v1/tunnels/{tunnel_id}/poll", "get")
	pollContent := responseContent(t, poll, "200")
	pollExample := pollContent["example"]
	pollSchema := mustMap(t, pollContent["schema"], "poll.200.schema")
	requireValidAgainstSchema(t, spec, pollSchema, pollExample, "poll example")

	pollJSON, err := json.Marshal(pollExample)
	if err != nil {
		t.Fatalf("marshal poll example: %v", err)
	}
	var envelope PolledCommandEnvelope
	if err := json.Unmarshal(pollJSON, &envelope); err != nil {
		t.Fatalf("decode poll example with Go envelope: %v", err)
	}
	if len(envelope.Commands) != 2 {
		t.Fatalf("expected two documented poll commands, got %d", len(envelope.Commands))
	}

	var rpc RawJSONRPCPolledCommand
	if err := json.Unmarshal(envelope.Commands[0], &rpc); err != nil {
		t.Fatalf("decode documented jsonrpc command: %v", err)
	}
	if rpc.CommandType != CommandTypeJSONRPC || len(rpc.JSONRPC) == 0 {
		t.Fatalf("unexpected documented jsonrpc command: %#v", rpc)
	}

	var termination RawSessionTerminationPolledCommand
	if err := json.Unmarshal(envelope.Commands[1], &termination); err != nil {
		t.Fatalf("decode documented session termination command: %v", err)
	}
	if termination.CommandType != CommandTypeSessionTermination {
		t.Fatalf("unexpected documented session command type %q", termination.CommandType)
	}

	response := operation(t, spec, "/v1/tunnels/{tunnel_id}/response", "post")
	requestContent := requestBodyContent(t, response)
	responseExample := requestContent["example"]
	responseSchema := mustMap(t, requestContent["schema"], "response.requestBody.schema")
	requireValidAgainstSchema(t, spec, responseSchema, responseExample, "response example")

	responseJSON, err := json.Marshal(responseExample)
	if err != nil {
		t.Fatalf("marshal response example: %v", err)
	}
	var payload TunnelResponsePayload
	if err := json.Unmarshal(responseJSON, &payload); err != nil {
		t.Fatalf("decode response example with Go payload: %v", err)
	}
	if payload.ResponseType != ResponsePayloadJSONRPC {
		t.Fatalf("expected documented response type %q, got %q", ResponsePayloadJSONRPC, payload.ResponseType)
	}
}

func TestGoResponsePayloadsMatchOpenAPI(t *testing.T) {
	spec, _ := readOpenAPISpec(t)
	response := operation(t, spec, "/v1/tunnels/{tunnel_id}/response", "post")
	requestContent := requestBodyContent(t, response)
	responseSchema := mustMap(t, requestContent["schema"], "response.requestBody.schema")

	payloads := []TunnelResponsePayload{
		{
			RequestID:    "req-jsonrpc",
			Channel:      "main",
			JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","id":"rpc-1","result":{}}`),
			ResponseCode: http.StatusOK,
			ResponseType: ResponsePayloadJSONRPC,
		},
		{
			RequestID:    "req-notify",
			Channel:      "main",
			JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`),
			ResponseCode: http.StatusOK,
			ResponseType: ResponsePayloadJSONRPCNotify,
		},
		{
			RequestID:    "req-ack",
			Channel:      "main",
			ResponseCode: http.StatusAccepted,
			ResponseType: ResponsePayloadNotifyAck,
		},
		{
			RequestID:    "req-termination",
			Channel:      "main",
			ResponseCode: http.StatusNoContent,
			ResponseType: ResponsePayloadSessionTermination,
		},
	}

	for _, payload := range payloads {
		payload := payload
		t.Run(string(payload.ResponseType), func(t *testing.T) {
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal Go response payload: %v", err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatalf("decode marshaled Go response payload: %v", err)
			}
			requireValidAgainstSchema(t, spec, responseSchema, value, "Go response payload")
		})
	}
}

func TestGoDiscriminatorsCoverPublishedOpenAPI(t *testing.T) {
	spec, _ := readOpenAPISpec(t)
	schemas := schemas(t, spec)

	responseType := mustMap(t, schemas["ResponsePayloadType"], "ResponsePayloadType")
	gotResponseTypes := stringSlice(t, responseType["enum"], "ResponsePayloadType.enum")
	wantResponseTypes := []string{
		string(ResponsePayloadJSONRPC),
		string(ResponsePayloadJSONRPCNotify),
		string(ResponsePayloadNotifyAck),
		string(ResponsePayloadSessionTermination),
	}
	if !reflect.DeepEqual(gotResponseTypes, wantResponseTypes) {
		t.Fatalf("published response discriminators do not match Go support: got %v want %v", gotResponseTypes, wantResponseTypes)
	}

	envelope := mustMap(t, schemas["PolledCommandList"], "PolledCommandList")
	properties := mustMap(t, envelope["properties"], "PolledCommandList.properties")
	commands := mustMap(t, properties["commands"], "PolledCommandList.properties.commands")
	items := mustMap(t, commands["items"], "PolledCommandList.properties.commands.items")
	discriminator := mustMap(t, items["discriminator"], "PolledCommandList.discriminator")
	mapping := mustMap(t, discriminator["mapping"], "PolledCommandList.discriminator.mapping")
	wantCommands := []string{
		string(CommandTypeJSONRPC),
		string(CommandTypeSessionTermination),
	}
	if got := mapKeys(mapping); !reflect.DeepEqual(got, wantCommands) {
		t.Fatalf("published command discriminators do not match Go support: got %v want %v", got, wantCommands)
	}
}

func readOpenAPISpec(t *testing.T) (map[string]any, []byte) {
	t.Helper()
	paths := []string{
		"api/tunnel-client/docs/openapi.json",
		"../../../docs/openapi.json",
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var spec map[string]any
		if err := json.Unmarshal(raw, &spec); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return spec, raw
	}
	t.Fatalf("openapi.json not found in %v", paths)
	return nil, nil
}

func operation(t *testing.T, spec map[string]any, path string, method string) map[string]any {
	t.Helper()
	paths := mustMap(t, spec["paths"], "paths")
	pathItem := mustMap(t, paths[path], "paths."+path)
	return mustMap(t, pathItem[method], method+" "+path)
}

func assertOperationID(t *testing.T, spec map[string]any, path string, method string, want string) {
	t.Helper()
	if got := mustString(t, operation(t, spec, path, method)["operationId"], method+" "+path+".operationId"); got != want {
		t.Fatalf("expected %s %s operationId %q, got %q", strings.ToUpper(method), path, want, got)
	}
}

func responseContent(t *testing.T, operation map[string]any, status string) map[string]any {
	t.Helper()
	responses := mustMap(t, operation["responses"], "responses")
	response := mustMap(t, responses[status], "responses."+status)
	content := mustMap(t, response["content"], "responses."+status+".content")
	return mustMap(t, content["application/json"], "responses."+status+".content.application/json")
}

func requestBodyContent(t *testing.T, operation map[string]any) map[string]any {
	t.Helper()
	requestBody := mustMap(t, operation["requestBody"], "requestBody")
	content := mustMap(t, requestBody["content"], "requestBody.content")
	return mustMap(t, content["application/json"], "requestBody.content.application/json")
}

func hasRequiredHeader(operation map[string]any, name string) bool {
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		return false
	}
	for _, item := range parameters {
		parameter, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if parameter["name"] == name && parameter["in"] == "header" && parameter["required"] == true {
			return true
		}
	}
	return false
}

func schemas(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()
	components := mustMap(t, spec["components"], "components")
	return mustMap(t, components["schemas"], "components.schemas")
}

func requireValidAgainstSchema(t *testing.T, spec map[string]any, schema map[string]any, value any, label string) {
	t.Helper()
	if err := validateAgainstSchema(spec, schema, value, "$"); err != nil {
		t.Fatalf("%s does not match OpenAPI: %v", label, err)
	}
}

func validateAgainstSchema(spec map[string]any, schema map[string]any, value any, path string) error {
	if ref, ok := schema["$ref"].(string); ok {
		resolved, err := resolveSchemaRef(spec, ref)
		if err != nil {
			return err
		}
		return validateAgainstSchema(spec, resolved, value, path)
	}

	if variants, ok := schema["anyOf"].([]any); ok {
		return validateVariant(spec, variants, value, path, false)
	}
	if variants, ok := schema["oneOf"].([]any); ok {
		return validateVariant(spec, variants, value, path, true)
	}

	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("%s: value %v does not equal const %v", path, value, constant)
	}
	if enumValues, ok := schema["enum"].([]any); ok {
		matched := false
		for _, enumValue := range enumValues {
			if reflect.DeepEqual(enumValue, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value %v is not in enum %v", path, value, enumValues)
		}
	}

	schemaType, _ := schema["type"].(string)
	switch schemaType {
	case "":
		return nil
	case "null":
		if value != nil {
			return fmt.Errorf("%s: expected null, got %T", path, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, value)
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s: expected integer, got %v", path, value)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s: expected number, got %T", path, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, value)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array, got %T", path, value)
		}
		itemSchema, hasItems := schema["items"].(map[string]any)
		if !hasItems {
			return nil
		}
		for index, item := range items {
			if err := validateAgainstSchema(spec, itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
		required, _ := schema["required"].([]any)
		for _, requiredValue := range required {
			name, ok := requiredValue.(string)
			if !ok {
				continue
			}
			if _, present := object[name]; !present {
				return fmt.Errorf("%s: missing required property %q", path, name)
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, propertyValue := range object {
			propertySchemaValue, known := properties[name]
			if known {
				propertySchema, ok := propertySchemaValue.(map[string]any)
				if !ok {
					return fmt.Errorf("%s.%s: invalid property schema", path, name)
				}
				if err := validateAgainstSchema(spec, propertySchema, propertyValue, path+"."+name); err != nil {
					return err
				}
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case bool:
				if !additional {
					return fmt.Errorf("%s: additional property %q is forbidden", path, name)
				}
			case map[string]any:
				if err := validateAgainstSchema(spec, additional, propertyValue, path+"."+name); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, schemaType)
	}
	return nil
}

func validateVariant(spec map[string]any, variants []any, value any, path string, exactlyOne bool) error {
	matches := 0
	var lastErr error
	for _, variant := range variants {
		variantSchema, ok := variant.(map[string]any)
		if !ok {
			continue
		}
		if err := validateAgainstSchema(spec, variantSchema, value, path); err != nil {
			lastErr = err
			continue
		}
		matches++
	}
	if matches == 0 {
		return fmt.Errorf("%s: no schema variant matched: %v", path, lastErr)
	}
	if exactlyOne && matches != 1 {
		return fmt.Errorf("%s: expected one schema variant, matched %d", path, matches)
	}
	return nil
}

func resolveSchemaRef(spec map[string]any, ref string) (map[string]any, error) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, fmt.Errorf("unsupported OpenAPI reference %q", ref)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("components is not an object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("components.schemas is not an object")
	}
	schema, ok := schemas[strings.TrimPrefix(ref, prefix)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing schema for %q", ref)
	}
	return schema, nil
}

func mustMap(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected object, got %T", path, value)
	}
	return result
}

func mustString(t *testing.T, value any, path string) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("%s: expected string, got %T", path, value)
	}
	return result
}

func stringSlice(t *testing.T, value any, path string) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s: expected array, got %T", path, value)
	}
	result := make([]string, 0, len(items))
	for index, item := range items {
		result = append(result, mustString(t, item, fmt.Sprintf("%s[%d]", path, index)))
	}
	return result
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
