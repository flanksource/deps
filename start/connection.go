package start

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"

	"github.com/flanksource/commons-db/models"
	cdbtypes "github.com/flanksource/commons-db/types"
	"github.com/flanksource/deps/start/state"
)

// hostPort returns the host-mapped primary port.
func hostPort(svc *ServiceContext) int {
	if svc.Opts.Port != 0 {
		return svc.Opts.Port
	}
	primary, _ := svc.Spec.PrimaryPort()
	return primary.Port
}

// templateData builds the variable map available to all ServiceSpec templates.
func templateData(svc *ServiceContext, host, release string) map[string]any {
	ports := map[string]int{}
	primary, _ := svc.Spec.PrimaryPort()
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	if svc.Opts.Port != 0 {
		ports[primary.Name] = svc.Opts.Port
	}

	version := strings.TrimPrefix(svc.Version, "v")
	parts := strings.SplitN(version, ".", 3)
	data := map[string]any{
		"name":         svc.Name,
		"version":      version,
		"tag":          svc.Version,
		"major":        parts[0],
		"minor":        "",
		"os":           svc.OS,
		"arch":         svc.Arch,
		"port":         hostPort(svc),
		"ports":        ports,
		"host":         host,
		"appDir":       svc.AppDir,
		"binDir":       svc.BinDir,
		"dataDir":      svc.DataDir,
		"runDir":       svc.RunDir,
		"passwordFile": svc.RunDir + "/.password",
		"username":     svc.Username,
		"password":     svc.Password,
		"database":     svc.Database,
		"release":      release,
		"namespace":    svc.Opts.Namespace,
	}
	if len(parts) > 1 {
		data["minor"] = parts[1]
	}
	return data
}

// render executes a ServiceSpec template, failing on unknown variables.
func render(name, tmpl string, data map[string]any) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("invalid template %s (%q): %w", name, tmpl, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render %s (%q): %w", name, tmpl, err)
	}
	return buf.String(), nil
}

// BuildConnection renders the service spec into a commons-db connection.
//
// binary/docker: localhost URL with inline credentials.
// helm: svc://<service>.<namespace>:<port> URL; credentials reference the
// chart secret via secret://<name>/<key> shorthand when the spec declares one.
func BuildConnection(svc *ServiceContext, st *state.State, kind RuntimeKind) (models.Connection, error) {
	if kind == RuntimeHelm {
		return buildHelmConnection(svc, st)
	}

	host := fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc))
	data := templateData(svc, host, "")
	url, err := render("url", svc.Spec.URL, data)
	if err != nil {
		return models.Connection{}, err
	}
	conn := models.Connection{
		Name:     svc.Name,
		Type:     svc.Spec.Type,
		URL:      url,
		Username: svc.Username,
		Password: svc.Password,
	}
	if err := renderProperties(&conn, svc.Spec.Properties, data); err != nil {
		return models.Connection{}, err
	}
	return conn, nil
}

func buildHelmConnection(svc *ServiceContext, st *state.State) (models.Connection, error) {
	helm := svc.Spec.Helm
	if helm == nil {
		return models.Connection{}, fmt.Errorf("service %s has no helm runtime", svc.Name)
	}
	data := templateData(svc, "", st.HelmRelease)

	svcRef := helm.Service
	if svcRef == nil {
		return models.Connection{}, fmt.Errorf("service %s helm runtime declares no service reference", svc.Name)
	}
	svcName, err := render("helm.service.name", svcRef.Name, data)
	if err != nil {
		return models.Connection{}, err
	}
	port := svcRef.Port
	if port == 0 {
		primary, _ := svc.Spec.PrimaryPort()
		port = primary.Port
	}

	conn := models.Connection{
		Name:      svc.Name,
		Namespace: svc.Opts.Namespace,
		Type:      svc.Spec.Type,
		URL:       fmt.Sprintf("svc://%s.%s:%d", svcName, svc.Opts.Namespace, port),
		Username:  svc.Username,
		Password:  svc.Password,
	}

	if helm.Secret != nil {
		secretName, err := render("helm.secret.name", helm.Secret.Name, data)
		if err != nil {
			return models.Connection{}, err
		}
		conn.Password = fmt.Sprintf("secret://%s/%s", secretName, helm.Secret.Key)
		if helm.Secret.UsernameKey != "" {
			conn.Username = fmt.Sprintf("secret://%s/%s", secretName, helm.Secret.UsernameKey)
		}
	}

	// {{.host}} in helm properties resolves to the in-cluster endpoint
	data["host"] = fmt.Sprintf("%s.%s:%d", svcName, svc.Opts.Namespace, port)
	if err := renderProperties(&conn, svc.Spec.Properties, data); err != nil {
		return models.Connection{}, err
	}
	return conn, nil
}

func renderProperties(conn *models.Connection, properties map[string]string, data map[string]any) error {
	if conn.Properties == nil && (len(properties) > 0 || data["database"] != "") {
		conn.Properties = cdbtypes.JSONStringMap{}
	}
	if db, _ := data["database"].(string); db != "" {
		conn.Properties["database"] = db
	}
	for k, v := range properties {
		rendered, err := render("property "+k, v, data)
		if err != nil {
			return err
		}
		conn.Properties[k] = rendered
	}
	return nil
}

// generatePassword returns a random 24-hex-char password.
func generatePassword() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(buf)
}
