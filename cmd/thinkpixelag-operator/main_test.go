package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestOperatorRejectsUnselectedEnvironment(t *testing.T) {
	t.Setenv("THINKPIXELAG_ENVIRONMENT", "production")
	var b bytes.Buffer
	if e := run(context.Background(), []string{"bootstrap"}, &b); e == nil || !strings.Contains(e.Error(), "local/test") || b.Len() != 0 {
		t.Fatal(e)
	}
}
