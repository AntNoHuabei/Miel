package app

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// 内置模型目录(单数据源):各服务商可选的模型列表,以及每个模型的
// “思考/推理”支持规格(开关 / 档位 / 常开 / 不支持)。
// 前端“添加模型选择”“思考档位控件”与后端参数映射都以此为准。
// 模型名随厂商版本演进,可随时编辑 catalog.json 增补。

//go:embed catalog.json
var catalogJSON []byte

// ReasoningSpec 描述一个模型的思考/推理支持方式。
type ReasoningSpec struct {
	Type   string   `json:"type"`             // toggle | effort | always | none
	Levels []string `json:"levels,omitempty"` // effort 档位(low/medium/high/max)
	Note   string   `json:"note,omitempty"`   // 面向用户的中文说明
}

// CatalogModel 目录中的单个模型。
type CatalogModel struct {
	ID            string        `json:"id"`
	Label         string        `json:"label"`
	Reasoning     ReasoningSpec `json:"reasoning"`
	Multimodal    bool          `json:"multimodal"` // 是否支持图片输入(视觉)
	ContextWindow int           `json:"contextWindow"`
}

// CatalogProvider 目录中的服务商及其模型列表。
type CatalogProvider struct {
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	BaseURL string         `json:"baseUrl"`
	Models  []CatalogModel `json:"models"`
}

var (
	catalogOnce      sync.Once
	cachedCatalog    []CatalogProvider
	cachedCatalogErr error
)

func loadCatalog() ([]CatalogProvider, error) {
	catalogOnce.Do(func() {
		var doc struct {
			Providers []CatalogProvider `json:"providers"`
		}
		cachedCatalogErr = json.Unmarshal(catalogJSON, &doc)
		if cachedCatalogErr == nil {
			cachedCatalog = doc.Providers
		}
	})
	return cachedCatalog, cachedCatalogErr
}

// ModelCatalog 返回内置模型目录(前端渲染模型下拉与思考档位)。
func (s *SettingsService) ModelCatalog() ([]CatalogProvider, error) {
	return loadCatalog()
}

// catalogLookup 按 kind 查目录服务商。
func catalogLookup(kind string) (CatalogProvider, bool) {
	catalog, err := loadCatalog()
	if err != nil {
		return CatalogProvider{}, false
	}
	kind = strings.TrimSpace(kind)
	for _, p := range catalog {
		if strings.EqualFold(p.Kind, kind) {
			return p, true
		}
	}
	return CatalogProvider{}, false
}

// modelReasoning 按 kind + 模型名查思考规格;找不到返回 none(无开关)。
func modelReasoning(kind, model string) ReasoningSpec {
	if p, ok := catalogLookup(kind); ok {
		model = strings.TrimSpace(model)
		for _, m := range p.Models {
			if strings.EqualFold(m.ID, model) {
				return m.Reasoning
			}
		}
	}
	return ReasoningSpec{Type: "none"}
}

// modelMultimodal 按 kind + 模型名查图片输入能力;目录收录时以目录为准。
func modelMultimodal(kind, model string) (multimodal bool, found bool) {
	if p, ok := catalogLookup(kind); ok {
		model = strings.TrimSpace(model)
		for _, m := range p.Models {
			if strings.EqualFold(m.ID, model) {
				return m.Multimodal, true
			}
		}
	}
	return false, false
}

func modelContextWindow(kind, model string) (int, bool) {
	if p, ok := catalogLookup(kind); ok {
		model = strings.TrimSpace(model)
		for _, m := range p.Models {
			if strings.EqualFold(m.ID, model) {
				return m.ContextWindow, m.ContextWindow > 0
			}
		}
	}
	return 0, false
}

func catalogContextWindow(kind, model string) int {
	window, _ := modelContextWindow(kind, model)
	return window
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// providerSupportsVision 判定某 Provider(其当前模型)是否支持图片输入:
// 目录收录的模型以目录 manifest 为准;自定义模型回落到手工配置的 multimodal。
func providerSupportsVision(p Provider) bool {
	if mm, ok := modelMultimodal(p.Kind, p.Model); ok {
		return mm
	}
	return p.Multimodal
}
