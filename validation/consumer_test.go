package validation_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A separate module must import public packages without access to internal/ or
// to the SDK's own test package. This check never contacts TypeSafe or a proxy.
func TestExternalGo123Consumer(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := "module example.com/independent-agent\n\ngo 1.23\n\nrequire github.com/PinableAgents/typesafe-sdk-go v0.0.0\n\nreplace github.com/PinableAgents/typesafe-sdk-go => " + "\"" + filepath.ToSlash(root) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(consumerSource), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	for _, value := range os.Environ() {
		name := strings.SplitN(value, "=", 2)[0]
		switch name {
		case "GOWORK", "GOPROXY", "GOSUMDB", "GOTOOLCHAIN", "GOFLAGS", "TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_COMPONENTS_LIVE", "TYPESAFE_LIVE_TEST":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent module failed: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "independent_agent_ok" {
		t.Fatal("consumer did not finish")
	}
}

const consumerSource = `package main
import (
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 typesafe "github.com/PinableAgents/typesafe-sdk-go"
 "github.com/PinableAgents/typesafe-sdk-go/contrib/agenttool"
)
func main() {
 s:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  if r.URL.Path=="/v1/models" {fmt.Fprint(w,"{\"models\":[{\"name\":\"fixture\",\"description\":\"not inference\",\"release_date\":\"2026-09-20\"}]}");return}
  fmt.Fprint(w,"{\"model\":\"fixture\",\"usage\":{},\"answers\":{\"write\":{\"type\":\"noul\",\"noul\":0.01}}}")
 }))
 defer s.Close()
 c,err:=typesafe.NewClient(typesafe.Config{APIKey:"local-consumer",BaseURL:s.URL,AllowInsecureHTTP:true})
 if err!=nil {panic(err)};defer c.Close()
 tools,err:=agenttool.New(c);if err!=nil {panic(err)}
 if len(agenttool.Definitions())!=3 {panic("definitions")}
 result:=tools.Call(context.Background(),agenttool.ListModels,[]byte("{}"))
 if !result.OK {panic("models")}
 result=tools.Call(context.Background(),agenttool.Evaluate,[]byte("{\"state\":\"explain only\",\"questions\":{\"write\":{\"type\":\"noul\",\"instructions\":\"Modifications requested?\"}}}"))
 if !result.OK || result.Data.(*typesafe.SystemOneResponse).Nouls["write"].Noul!=0.01 {panic("evaluate")}
 fmt.Println("independent_agent_ok")
}
`
