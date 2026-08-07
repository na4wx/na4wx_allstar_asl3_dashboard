package cloudagent

import (
	"context"
	"encoding/json"
	"testing"

	"hamvoipconfiggui-asl3/internal/config"
)

func TestActionRegistrationLoadWhenNoneExists(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "registration.load", params)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil for a node with no registration", result)
	}
}

func TestActionRegistrationSaveLoadRemove(t *testing.T) {
	a := newConfigTestAgent(t)

	saveParams, _ := json.Marshal(map[string]string{"number": "1999", "password": "secret123", "server": ""})
	if _, err := a.dispatch(context.Background(), "registration.save", saveParams); err != nil {
		t.Fatalf("save dispatch error = %v", err)
	}

	loadParams, _ := json.Marshal(map[string]string{"number": "1999"})
	result, err := a.dispatch(context.Background(), "registration.load", loadParams)
	if err != nil {
		t.Fatalf("load dispatch error = %v", err)
	}
	reg, ok := result.(config.Registration)
	if !ok {
		t.Fatalf("result type = %T, want config.Registration", result)
	}
	if reg.Password != "secret123" {
		t.Errorf("Password = %q, want secret123", reg.Password)
	}
	if reg.Server != registrationDefaultServer {
		t.Errorf("Server = %q, want default %q applied for blank input", reg.Server, registrationDefaultServer)
	}

	removeParams, _ := json.Marshal(map[string]string{"number": "1999"})
	if _, err := a.dispatch(context.Background(), "registration.remove", removeParams); err != nil {
		t.Fatalf("remove dispatch error = %v", err)
	}
	result, err = a.dispatch(context.Background(), "registration.load", loadParams)
	if err != nil {
		t.Fatalf("load-after-remove dispatch error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil after remove", result)
	}
}

func TestActionRegistrationSaveRequiresPassword(t *testing.T) {
	a := newConfigTestAgent(t)
	params, _ := json.Marshal(map[string]string{"number": "1999", "password": "", "server": "register.allstarlink.org"})
	if _, err := a.dispatch(context.Background(), "registration.save", params); err == nil {
		t.Fatal("expected an error for a blank password")
	}
}
