package tests

import (
	"fmt"
	"net/http"
)

func subTestProto(g *testGroup) {
	g.regSubTest("HTTP CORS", proto_HTTP_CORS_test)
}

func proto_HTTP_CORS_test(mc *mockServer) error {
	// Make CORS request for GET /SERVER
	morigin := "http://my-test-origin"
	url := fmt.Sprintf("http://127.0.0.1:%d/SERVER", mc.port)
	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Origin", morigin)
	req.Header.Add("Access-Control-Request-Method", "GET")
	req.Header.Add("Access-Control-Request-Headers", "Authorization")
	resp, err := http.DefaultClient.Do(req)

	// Validate CORS response
	if err != nil {
		return err
	}
	if resp.StatusCode != 204 {
		return fmt.Errorf("expected http stuats '204', got '%d'", resp.StatusCode)
	}
	origin := resp.Header.Get("Access-Control-Allow-Origin")
	methods := resp.Header.Get("Access-Control-Allow-Methods")
	headers := resp.Header.Get("Access-Control-Allow-Headers")
	if !(origin == "*" || origin == morigin) {
		return fmt.Errorf("expected http access-control-allow-origin value '*', got '%s'", origin)
	}
	if methods != "POST, GET, OPTIONS" {
		return fmt.Errorf("expected http access-control-allow-Methods value 'POST, GET, OPTIONS', got '%s'", methods)
	}
	if headers != "*, Authorization" {
		return fmt.Errorf("expected http access-control-allow-headers value '*, Authorization', got '%s'", headers)
	}

	// Make the actual request now
	resp, err = http.Get(url)
	if err != nil {
		return err
	}
	origin = resp.Header.Get("Access-Control-Allow-Origin")
	if !(origin == "*" || origin == morigin) {
		return fmt.Errorf("expected http access-control-allow-origin value '*', got '%s'", origin)
	}

	return nil
}
