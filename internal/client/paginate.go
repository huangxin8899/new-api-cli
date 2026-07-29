package client

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/QuantumNous/new-api-cli/errs"
)

// Page 是 New API 列表接口的分页信封（common.PageInfo）。
type Page struct {
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Total    int             `json:"total"`
	Items    json.RawMessage `json:"items"`
}

// MaxPageSize 是服务端硬上限（common.GetPageQuery 会截断到 100）。
const MaxPageSize = 100

// ListOptions 是列表类命令的通用翻页参数。
type ListOptions struct {
	Page     int
	PageSize int
	// All 为 true 时自动翻页直到取完或达到 Limit。
	All bool
	// Limit 是 --all 模式下最多拉取的记录数，0 表示不限。
	Limit int
	// MaxPages 是 --all 模式下最多翻的页数，防止无界循环。
	MaxPages int
}

// Normalize 填充默认值并把 page_size 限制在服务端上限内。
func (o *ListOptions) Normalize() {
	if o.Page <= 0 {
		o.Page = 1
	}
	if o.PageSize <= 0 {
		o.PageSize = 20
	}
	if o.PageSize > MaxPageSize {
		o.PageSize = MaxPageSize
	}
	if o.MaxPages <= 0 {
		o.MaxPages = 100
	}
}

// ListResult 是翻页后的聚合结果。
type ListResult struct {
	Items []any
	Total int
	Page  int
	// Truncated 表示因为 Limit / MaxPages 提前停止，还有更多数据没取。
	Truncated bool
}

// List 拉取一页或（All 模式下）多页数据。
//
// 服务端对未知的分页对象也可能直接返回数组（部分接口如 /api/group/），
// 这里两种形态都接受。
func (c *Client) List(ctx context.Context, req Request, opts ListOptions) (*ListResult, error) {
	opts.Normalize()

	result := &ListResult{Page: opts.Page}
	page := opts.Page

	for fetched := 0; ; fetched++ {
		if req.Query == nil {
			req.Query = url.Values{}
		}
		q := cloneValues(req.Query)
		q.Set("p", strconv.Itoa(page))
		q.Set("page_size", strconv.Itoa(opts.PageSize))
		pageReq := req
		pageReq.Query = q

		resp, err := c.Do(ctx, pageReq)
		if err != nil {
			return nil, err
		}

		items, total, err := decodeItems(resp.Data)
		if err != nil {
			return nil, err
		}
		if total > result.Total {
			result.Total = total
		}
		result.Items = append(result.Items, items...)

		if !opts.All {
			return result, nil
		}
		if opts.Limit > 0 && len(result.Items) >= opts.Limit {
			result.Items = result.Items[:opts.Limit]
			result.Truncated = total > len(result.Items)
			return result, nil
		}
		// 服务端返回不足一页即到底；total 已知时也可提前收敛。
		if len(items) < opts.PageSize {
			return result, nil
		}
		if total > 0 && len(result.Items) >= total {
			return result, nil
		}
		if fetched+1 >= opts.MaxPages {
			result.Truncated = true
			return result, nil
		}
		page++
	}
}

// decodeItems 从 data 中取出记录列表，兼容分页对象与裸数组。
func decodeItems(data json.RawMessage) ([]any, int, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, 0, nil
	}
	var page Page
	if err := json.Unmarshal(data, &page); err == nil && page.Items != nil {
		var items []any
		if err := json.Unmarshal(page.Items, &items); err != nil {
			return nil, 0, errs.NewAPIError(errs.SubtypeBadResponse,
				"分页 items 不是数组: %v", err)
		}
		return items, page.Total, nil
	}
	var items []any
	if err := json.Unmarshal(data, &items); err == nil {
		return items, len(items), nil
	}
	// data 是单个对象（例如某些接口不分页），当成一条记录。
	var single any
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, 0, errs.NewAPIError(errs.SubtypeBadResponse,
			"无法解析列表响应: %v", err)
	}
	return []any{single}, 1, nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, values := range v {
		for _, value := range values {
			out.Add(k, value)
		}
	}
	return out
}
