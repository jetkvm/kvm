package kvm

import (
	"bytes"
	"encoding/json"
	"html/template"
	"runtime"
	"strings"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/prometheus/common/version"
)

var versionInfoTmpl = `
JetKVM Application, version {{.version}} (branch: {{.branch}}, revision: {{.revision}})
  build date:       {{.buildDate}}
  go version:       {{.goVersion}}
  platform:         {{.platform}}

{{if .lvglVersion}}
LVGL version {{.lvglVersion}}
{{end}}
`

func GetVersionData(isJson bool) ([]byte, error) {
	version.Version = GetBuiltAppVersion()

	m := map[string]string{
		"version":   version.Version,
		"revision":  version.GetRevision(),
		"branch":    version.Branch,
		"buildDate": version.BuildDate,
		"goVersion": version.GoVersion,
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
	}

	lvglVersion := native.GetLVGLVersion()
	if lvglVersion != "" {
		m["lvglVersion"] = lvglVersion
	}

	if isJson {
		jsonData, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		return jsonData, nil
	}

	t := template.Must(
		template.New("version").Parse(
			strings.TrimSpace(versionInfoTmpl),
		),
	)

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "version", m); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
