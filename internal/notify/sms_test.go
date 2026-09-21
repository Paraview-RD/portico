package notify

import (
	"context"
	"errors"
	"testing"
)

func TestNotConfiguredSMSAlwaysFails(t *testing.T) {
	var s SMSSender = NotConfiguredSMS{}
	err := s.Send(context.Background(), "+8613800138000", SMSKindLoginCode,
		map[string]string{"Code": "123456", "Minutes": "5"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}
