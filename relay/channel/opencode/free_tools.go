package opencode

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OpenCode 2.x uses shell rather than bash. Empty parameter-schema stubs no
// longer satisfy the free endpoint. Only missing tools use these definitions;
// a caller's tool schema and description must retain their original meaning.
var freeCoreTools = []string{"shell", "edit", "glob", "grep", "read"}

// Captured definitions from jasonxu114514/opencode2api a6601da (GPL-3.0),
// originally OpenCode (MIT). See opencode_tools.LICENSE for source and licenses.
//
//go:embed opencode_tools.json
var freeToolsJSON []byte

type freeToolDefinition struct {
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

var loadFreeTools = sync.OnceValues(func() (map[string]freeToolDefinition, error) {
	var definitions map[string]freeToolDefinition
	if err := common.Unmarshal(freeToolsJSON, &definitions); err != nil {
		return nil, fmt.Errorf("decode OpenCode tool definitions: %w", err)
	}
	for _, name := range freeCoreTools {
		if definition, ok := definitions[name]; !ok || definition.Description == "" || len(definition.Parameters) == 0 {
			return nil, fmt.Errorf("missing OpenCode tool definition: %s", name)
		}
	}
	return definitions, nil
})
