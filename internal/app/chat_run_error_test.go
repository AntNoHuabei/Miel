package app

import "testing"

func TestNormalizeChatRunError(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		message string
		want    string
	}{
		{name: "preserves provider code", code: "insufficient_quota", message: "quota exhausted", want: "insufficient_quota"},
		{name: "extracts provider JSON code before HTTP status", message: `POST "https://openrouter.ai": 429 Too Many Requests {"error":{"message":"limited","code":"rate_limit_exceeded"}}`, want: "rate_limit_exceeded"},
		{name: "extracts numeric provider JSON code", message: `POST "https://example.test": 400 Bad Request {"error":{"message":"bad request","code":1007}}`, want: "1007"},
		{name: "extracts HTTP status", message: `POST "https://openrouter.ai": 429 Too Many Requests`, want: "429"},
		{name: "timeout", message: "context deadline exceeded", want: "timeout"},
		{name: "cancelled", message: "context canceled", want: "cancelled"},
		{name: "network", message: "dial tcp: connection refused", want: "network_error"},
		{name: "fallback", message: "unexpected provider response", want: "unknown_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeChatRunError(test.code, test.message)
			if got.Code != test.want || got.Message != test.message {
				t.Fatalf("normalized error = %#v, want code %q and original message", got, test.want)
			}
		})
	}
}
