package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
)

// mockSite 是一个最小的 New API 管理接口替身。它复刻了真实服务端两个
// 关键行为：业务失败也返回 HTTP 200（success:false），列表走
// {items,page,page_size,total} 分页信封。
type mockSite struct {
	*httptest.Server
	// mu 保护 requests：httptest 每个请求各起一个 goroutine。
	mu sync.Mutex
	// requests 按顺序记录收到的请求，供断言查询参数与请求头。
	requests []recordedRequest
}

// recorded 返回请求记录的快照。
func (s *mockSite) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

func newMockSite(t *testing.T, routes map[string]http.HandlerFunc) *mockSite {
	t.Helper()
	site := &mockSite{}
	mux := http.NewServeMux()
	for pattern, handler := range routes {
		h := handler
		mux.HandleFunc(pattern, h)
	}
	site.Server = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			if r.Body != nil {
				body, _ = readAllLimited(r)
				// 记录用掉了 Body，必须补回去，否则路由处理器读到空。
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			site.mu.Lock()
			site.requests = append(site.requests, recordedRequest{
				Method: r.Method,
				Path:   r.URL.Path,
				Query:  r.URL.RawQuery,
				Auth:   r.Header.Get("Authorization"),
				Body:   string(body),
			})
			site.mu.Unlock()
			mux.ServeHTTP(w, r)
		}))
	t.Cleanup(site.Close)
	return site
}

func readAllLimited(r *http.Request) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(http.MaxBytesReader(nil, r.Body, 1<<20))
	return buf.Bytes(), err
}

// ok 写一个成功信封。
func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true, "message": "", "data": data,
	})
}

// fail 写一个业务失败信封 —— 注意仍是 HTTP 200，这是 New API 的真实行为。
func fail(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false, "message": message,
	})
}

// page 写一个分页信封。
func page(w http.ResponseWriter, items []any, total, p, size int) {
	ok(w, map[string]any{
		"items": items, "total": total, "page": p, "page_size": size,
	})
}

// run 在进程内驱动完整命令树，返回 stdout、stderr 与退出码。
func run(t *testing.T, site *mockSite, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("NEW_API_CLI_HOME", t.TempDir())
	t.Setenv("NEW_API_PROFILE", "")
	t.Setenv("NEW_API_USER_ID", "")
	if site != nil {
		t.Setenv("NEW_API_BASE_URL", site.URL)
		t.Setenv("NEW_API_TOKEN", "test-pat-token")
	} else {
		t.Setenv("NEW_API_BASE_URL", "")
		t.Setenv("NEW_API_TOKEN", "")
	}

	var stdout, stderr bytes.Buffer
	streams := cmdutil.IOStreams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	f := cmdutil.NewFactory(streams, &cmdutil.GlobalFlags{})
	root := NewRootCmd(f)
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := 0
	if err := root.Execute(); err != nil {
		code = handleError(f, err)
	}
	return stdout.String(), stderr.String(), code
}

// envelope 解析成功信封。
func envelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, stdout)
	}
	if env["ok"] != true {
		t.Fatalf("期望 ok:true，得到: %s", stdout)
	}
	return env
}

func TestTokenListRendersItemsAndPagination(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			page(w, []any{
				map[string]any{"id": 1, "name": "prod", "key": "sk-1234**********abcd"},
				map[string]any{"id": 2, "name": "dev", "key": "sk-5678**********efgh"},
			}, 2, 1, 20)
		},
	})

	stdout, _, code := run(t, site, "token", "list")
	if code != 0 {
		t.Fatalf("退出码 = %d, stdout=%s", code, stdout)
	}
	env := envelope(t, stdout)

	items, ok := env["data"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("data 应是 2 条记录: %s", stdout)
	}
	meta, ok := env["meta"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 meta: %s", stdout)
	}
	if meta["total"] != float64(2) {
		t.Errorf("meta.total = %v, 期望 2", meta["total"])
	}

	// 列表命令必须把翻页参数编成服务端认识的 p / page_size。
	req := site.recorded()[0]
	if !strings.Contains(req.Query, "p=1") || !strings.Contains(req.Query, "page_size=20") {
		t.Errorf("翻页参数不符: %q", req.Query)
	}
	if req.Auth != "Bearer test-pat-token" {
		t.Errorf("Authorization = %q", req.Auth)
	}
}

func TestBusinessFailureIsErrorNotSuccess(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			// 真实服务端在业务失败时同样返回 HTTP 200。
			fail(w, "无权进行此操作")
		},
	})

	stdout, stderr, code := run(t, site, "token", "list")
	if code == 0 {
		t.Fatalf("业务失败必须非零退出，stdout=%s", stdout)
	}
	if strings.Contains(stdout, `"ok": true`) {
		t.Errorf("失败不该往 stdout 写成功信封: %s", stdout)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v\n%s", err, stderr)
	}
	if env.OK {
		t.Error("错误信封的 ok 应为 false")
	}
	// "无权" 应被归类成权限问题而非泛化的 api 错误。
	if env.Error.Type != "auth" {
		t.Errorf("type = %q, 期望 auth（消息含「无权」）", env.Error.Type)
	}
}

func TestPageAllFollowsPagesUntilExhausted(t *testing.T) {
	var hits int
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/channel/": func(w http.ResponseWriter, r *http.Request) {
			hits++
			p := r.URL.Query().Get("p")
			switch p {
			case "1":
				items := make([]any, 100)
				for i := range items {
					items[i] = map[string]any{"id": i + 1, "name": fmt.Sprintf("ch-%d", i+1)}
				}
				page(w, items, 150, 1, 100)
			case "2":
				items := make([]any, 50)
				for i := range items {
					items[i] = map[string]any{"id": i + 101, "name": fmt.Sprintf("ch-%d", i+101)}
				}
				page(w, items, 150, 2, 100)
			default:
				page(w, []any{}, 150, 3, 100)
			}
		},
	})

	stdout, _, code := run(t, site, "channel", "list", "--all", "--page-size", "100")
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	env := envelope(t, stdout)
	items := env["data"].([]any)
	if len(items) != 150 {
		t.Errorf("--all 应取回 150 条，实得 %d", len(items))
	}
	if hits != 2 {
		t.Errorf("应只需 2 次请求（第 2 页不满即到底），实际 %d", hits)
	}
}

func TestPageAllRespectsLimit(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/channel/": func(w http.ResponseWriter, r *http.Request) {
			items := make([]any, 100)
			for i := range items {
				items[i] = map[string]any{"id": i + 1}
			}
			page(w, items, 1000, 1, 100)
		},
	})

	stdout, _, code := run(t, site, "channel", "list", "--all", "--limit", "30", "--page-size", "100")
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	env := envelope(t, stdout)
	if items := env["data"].([]any); len(items) != 30 {
		t.Errorf("--limit 30 应截断到 30 条，实得 %d", len(items))
	}
}

func TestHighRiskWriteRefusesWithoutYes(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/channel/1": func(w http.ResponseWriter, r *http.Request) {
			t.Error("未确认就发出了删除请求")
			ok(w, nil)
		},
	})

	stdout, stderr, code := run(t, site, "channel", "delete", "1")
	if code == 0 {
		t.Fatalf("缺少 --yes 时必须失败，stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "confirm_required") {
		t.Errorf("应报 confirm_required: %s", stderr)
	}
	// 关键断言：确认门禁必须在发请求之前拦下。
	if len(site.recorded()) != 0 {
		t.Errorf("确认前不该有任何请求，实际 %d 个", len(site.recorded()))
	}
}

func TestHighRiskWriteProceedsWithYes(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/channel/1": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				t.Errorf("方法 = %s, 期望 DELETE", r.Method)
			}
			ok(w, nil)
		},
	})

	stdout, stderr, code := run(t, site, "channel", "delete", "1", "--yes")
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	envelope(t, stdout)
	if len(site.recorded()) != 1 {
		t.Fatalf("应发出 1 个请求，实际 %d", len(site.recorded()))
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/channel/1": func(w http.ResponseWriter, r *http.Request) {
			t.Error("--dry-run 不该真正发请求")
		},
	})

	stdout, _, code := run(t, site, "channel", "delete", "1", "--yes", "--dry-run")
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	var env struct {
		OK     bool `json:"ok"`
		DryRun bool `json:"dry_run"`
		Data   struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, stdout)
	}
	if !env.DryRun {
		t.Error("信封应标记 dry_run:true")
	}
	if env.Data.Method != "DELETE" || env.Data.Path != "/api/channel/1" {
		t.Errorf("预演内容不符: %+v", env.Data)
	}
	if len(site.recorded()) != 0 {
		t.Errorf("--dry-run 下不该有请求，实际 %d 个", len(site.recorded()))
	}
}

func TestJQFiltersData(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			page(w, []any{
				map[string]any{"id": 1, "name": "prod"},
				map[string]any{"id": 2, "name": "dev"},
			}, 2, 1, 20)
		},
	})

	stdout, _, code := run(t, site, "token", "list", "--jq", ".[].name")
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	env := envelope(t, stdout)
	names, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("jq 结果应是数组: %s", stdout)
	}
	if len(names) != 2 || names[0] != "prod" || names[1] != "dev" {
		t.Errorf("jq 过滤结果不符: %v", names)
	}
}

func TestGenericAPICommandNormalizesPath(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/status": func(w http.ResponseWriter, r *http.Request) {
			ok(w, map[string]any{"version": "v0.9.0"})
		},
	})

	// 省略 /api 前缀应被自动补上。
	stdout, stderr, code := run(t, site, "api", "GET", "/status")
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	envelope(t, stdout)
	if site.recorded()[0].Path != "/api/status" {
		t.Errorf("路径 = %q, 期望 /api/status", site.recorded()[0].Path)
	}
}

func TestGenericAPICommandSendsParamsAndBody(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/option/": func(w http.ResponseWriter, r *http.Request) {
			ok(w, nil)
		},
	})

	_, stderr, code := run(t, site,
		"api", "PUT", "/api/option/",
		"--params", `{"scope":"all"}`,
		"--data", `{"key":"AutomaticDisableChannelEnabled","value":"true"}`,
	)
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	req := site.recorded()[0]
	if req.Method != "PUT" {
		t.Errorf("方法 = %s", req.Method)
	}
	if !strings.Contains(req.Query, "scope=all") {
		t.Errorf("查询参数 = %q", req.Query)
	}
	if !strings.Contains(req.Body, "AutomaticDisableChannelEnabled") {
		t.Errorf("请求体 = %q", req.Body)
	}
}

func TestGenericAPICommandRejectsBadJSON(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{})

	_, stderr, code := run(t, site, "api", "POST", "/api/channel/", "--data", "{not json")
	if code == 0 {
		t.Fatal("非法 JSON 应当失败")
	}
	if !strings.Contains(stderr, "invalid_json") {
		t.Errorf("应报 invalid_json: %s", stderr)
	}
	// 本地校验必须在发请求之前完成。
	if len(site.recorded()) != 0 {
		t.Errorf("非法输入不该发出请求，实际 %d 个", len(site.recorded()))
	}
}

func TestHTTPForbiddenMapsToExitForbidden(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/user/": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"code":    "AUTH_INSUFFICIENT_PRIVILEGE",
				"message": "权限不足",
			})
		},
	})

	_, stderr, code := run(t, site, "user", "list")
	if code != 5 {
		t.Errorf("退出码 = %d, 期望 5 (forbidden)，stderr=%s", code, stderr)
	}
}

func TestTableFormatWritesRowsToStdout(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			page(w, []any{
				map[string]any{"id": 1, "name": "prod", "status": 1},
			}, 1, 1, 20)
		},
	})

	stdout, _, code := run(t, site, "token", "list", "--format", "table", "--columns", "id,name")
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.Contains(stdout, "prod") {
		t.Errorf("表格应含数据行: %s", stdout)
	}
	if strings.Contains(stdout, `"ok"`) {
		t.Errorf("table 模式不该输出 JSON 信封: %s", stdout)
	}
}

func TestUpdateMergesOntoServerCopy(t *testing.T) {
	var putBody string
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/token/12": func(w http.ResponseWriter, r *http.Request) {
			// 服务端返回的 key 是掩码，且带一个 CLI 不认识的新字段。
			ok(w, map[string]any{
				"id": 12, "name": "old", "key": "sk-1234**********abcd",
				"group": "vip", "remain_quota": 500, "unlimited_quota": false,
				"future_field": "must-survive",
			})
		},
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			body, _ := readAllLimited(r)
			putBody = string(body)
			ok(w, map[string]any{"id": 12, "name": "new"})
		},
	})

	_, stderr, code := run(t, site, "token", "update", "12", "--name", "new")
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(putBody), &sent); err != nil {
		t.Fatalf("PUT 请求体不是合法 JSON: %v\n%s", err, putBody)
	}
	if sent["name"] != "new" {
		t.Errorf("name 应更新为 new: %v", sent["name"])
	}
	// 未指定的字段必须保留服务端原值，这是整体替换语义下的关键保护。
	if sent["group"] != "vip" {
		t.Errorf("未指定的 group 应保持 vip: %v", sent["group"])
	}
	// 服务端新增字段应原样穿过，不被 CLI 的结构体定义丢弃。
	if sent["future_field"] != "must-survive" {
		t.Errorf("未知字段应原样保留: %v", sent["future_field"])
	}
	// 掩码 key 绝不能写回，否则会把真 key 覆盖成掩码串。
	if _, present := sent["key"]; present {
		t.Errorf("掩码 key 不该出现在更新请求里: %s", putBody)
	}
}

func TestStatusWorksWithoutToken(t *testing.T) {
	site := newMockSite(t, map[string]http.HandlerFunc{
		"/api/status": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("公开接口不该带 Authorization")
			}
			ok(w, map[string]any{"version": "v0.9.0", "start_time": 1234567890})
		},
	})

	// 只给 base-url，不给 token —— status 是无需认证的公开接口。
	t.Setenv("NEW_API_CLI_HOME", t.TempDir())
	t.Setenv("NEW_API_TOKEN", "")
	t.Setenv("NEW_API_PROFILE", "")
	var stdout, stderr bytes.Buffer
	streams := cmdutil.IOStreams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	f := cmdutil.NewFactory(streams, &cmdutil.GlobalFlags{})
	root := NewRootCmd(f)
	root.SetArgs([]string{"status", "--base-url", site.URL})
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := 0
	if err := root.Execute(); err != nil {
		code = handleError(f, err)
	}
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr.String())
	}
	envelope(t, stdout.String())
}
