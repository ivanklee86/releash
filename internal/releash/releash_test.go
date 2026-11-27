package releash

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	for name, r := range map[string]*Releash{
		"New":           New(),
		"NewWithConfig": NewWithConfig(Config{}),
	} {
		t.Run(name+" writes to stdout and stderr", func(t *testing.T) {
			assert.Same(t, os.Stdout, r.Out)
			assert.Same(t, os.Stderr, r.Err)
			assert.NotNil(t, r.Config)
		})
	}
}
