package external

type ErrorDetails struct {
	// Message corresponds to the JSON schema field "message".
	Message string `json:"message" msgpack:"message"`

	// StackTrace corresponds to the JSON schema field "stack_trace".
	StackTrace string `json:"stack_trace" msgpack:"stack_trace"`

	// Type corresponds to the JSON schema field "type".
	Type string `json:"type" msgpack:"type"`
}
