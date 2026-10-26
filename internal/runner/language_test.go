package runner

import (
	"testing"

	"testdesk/internal/scripts"
)

func TestCheckLanguage(t *testing.T) {
	cases := []struct {
		name    string
		sc      scripts.Script
		wantErr bool
	}{
		{"python code as python", scripts.Script{Language: scripts.Python, Code: "# 由 TestDesk 录制生成\nprint(1)"}, false},
		{"js code as js", scripts.Script{Language: scripts.JavaScript, Code: "// 由 TestDesk 录制生成\nimport x from 'y'"}, false},
		{"python code as js", scripts.Script{Language: scripts.JavaScript, Code: "# 由 TestDesk 录制生成\nprint(1)"}, true},
		{"js code as python", scripts.Script{Language: scripts.Python, Code: "// 由 TestDesk 录制生成\nconsole.log(1)"}, true},
		{"shebang as js is ok", scripts.Script{Language: scripts.JavaScript, Code: "#!/usr/bin/env node\nconsole.log(1)"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkLanguage(c.sc)
			if (err != nil) != c.wantErr {
				t.Fatalf("checkLanguage() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}
