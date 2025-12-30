package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AdbGatewayClient ADB Gateway HTTP 客户端
type AdbGatewayClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewAdbGatewayClient 创建新的 ADB Gateway 客户端
func NewAdbGatewayClient(baseURL string, token string) *AdbGatewayClient {
	return &AdbGatewayClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Mapping 映射响应结构
type Mapping struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	Listen    string `json:"listen"`
	Upstream  string `json:"upstream"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
	CreatedAt string `json:"created_at"`
}

// MappingSpec 映射创建/更新请求结构
type MappingSpec struct {
	ID              string `json:"id,omitempty"`              // 仅更新时必填
	ProjectID       string `json:"project_id,omitempty"`      // 可选
	FromID          string `json:"from_id,omitempty"`         // 可选
	ToID            string `json:"to_id,omitempty"`           // 可选
	Name            string `json:"name"`                      // 必填
	Note            string `json:"note,omitempty"`            // 可选
	Listen          string `json:"listen,omitempty"`          // 可选
	Upstream        string `json:"upstream,omitempty"`        // 可选
	ForceDisconnect bool   `json:"force_disconnect,omitempty"` // 可选，仅更新时有效
}

// AdbCommandLogEntry ADB 命令日志条目
type AdbCommandLogEntry struct {
	Time      string `json:"time"`
	MappingID string `json:"mapping_id"`
	ProjectID string `json:"project_id"`
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	Dir       string `json:"dir"`
	ConnID    string `json:"conn_id"`
	Desc      string `json:"desc"`
}

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatusResponse 状态响应结构
type StatusResponse struct {
	Status string `json:"status"`
}

// doRequest 执行 HTTP 请求
func (c *AdbGatewayClient) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	return resp, nil
}

// parseErrorResponse 解析错误响应
func (c *AdbGatewayClient) parseErrorResponse(resp *http.Response) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return fmt.Errorf("%s", errResp.Error)
}

// ListMappings 查询所有映射
func (c *AdbGatewayClient) ListMappings() ([]Mapping, error) {
	resp, err := c.doRequest("GET", "/api/mappings", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var mappings []Mapping
	if err := json.NewDecoder(resp.Body).Decode(&mappings); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return mappings, nil
}

// GetMapping 查询单个映射
func (c *AdbGatewayClient) GetMapping(id string) (*Mapping, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/api/mappings/%s", id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var mapping Mapping
	if err := json.NewDecoder(resp.Body).Decode(&mapping); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &mapping, nil
}

// CreateMapping 创建映射
func (c *AdbGatewayClient) CreateMapping(spec MappingSpec) (*Mapping, error) {
	resp, err := c.doRequest("POST", "/api/mappings/create", spec)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var mapping Mapping
	if err := json.NewDecoder(resp.Body).Decode(&mapping); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &mapping, nil
}

// UpdateMapping 更新映射
func (c *AdbGatewayClient) UpdateMapping(spec MappingSpec) (*Mapping, error) {
	resp, err := c.doRequest("POST", "/api/mappings/update", spec)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var mapping Mapping
	if err := json.NewDecoder(resp.Body).Decode(&mapping); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &mapping, nil
}

// RemoveMapping 删除映射
func (c *AdbGatewayClient) RemoveMapping(id string) error {
	reqBody := map[string]string{
		"id": id,
	}

	resp, err := c.doRequest("POST", "/api/mappings/remove", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseErrorResponse(resp)
	}

	return nil
}

// GetAdbCommandLogs 查询 ADB 命令日志
func (c *AdbGatewayClient) GetAdbCommandLogs(mappingID, start, end string) ([]AdbCommandLogEntry, error) {
	params := url.Values{}
	params.Set("mapping_id", mappingID)
	params.Set("start", start)
	params.Set("end", end)

	path := "/api/logs/adb-commands?" + params.Encode()

	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var logs []AdbCommandLogEntry
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return logs, nil
}

