package config_test

import (
	"testing"

	"azugo.io/core/validation"
	"github.com/go-quicktest/qt"

	"github.com/gmb-sig/go-platform-kit/config"
)

// newBase returns a BaseConfiguration with the standard fields set to valid
// values, so individual tests can invalidate one field at a time.
func newBase() *config.BaseConfiguration {
	c := config.New()
	c.ServiceName = "document-svc"
	c.Environment = "prod"

	return c
}

func TestValidate_OK(t *testing.T) {
	v := validation.New()
	qt.Assert(t, qt.IsNil(newBase().Validate(v)))
}

func TestValidate_MissingServiceName(t *testing.T) {
	v := validation.New()
	c := newBase()
	c.ServiceName = ""

	// Fail-fast: a missing required base field must fail Validate at startup.
	qt.Assert(t, qt.IsNotNil(c.Validate(v)))
}

func TestValidate_InvalidEnvironment(t *testing.T) {
	v := validation.New()
	c := newBase()
	c.Environment = "production" // not one of local|dev|staging|prod

	qt.Assert(t, qt.IsNotNil(c.Validate(v)))
}

func TestValidate_EnvironmentEnum(t *testing.T) {
	for _, env := range []string{"local", "dev", "staging", "prod"} {
		v := validation.New()
		c := newBase()
		c.Environment = env
		qt.Check(t, qt.IsNil(c.Validate(v)), qt.Commentf("environment %q should be valid", env))
	}
}
