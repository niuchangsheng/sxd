// Package config 提供系统配置读取、YAML 反序列化、环境变量展开与参数校验。
//
// 本文件实现按区服 (server_id) 覆盖全局调度策略的 YAML 深度合并能力。
//
// @author Ateng
// @since 2026-11-06
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// EffectiveScheduler 返回指定区服合并 server_overrides 后生效的调度配置。
//
// 合并规则:
//   - 映射 (mapping) 逐键递归合并，因此 server_overrides.s0.routines 只覆盖列出的玩法，
//     未列出的玩法继续继承全局开关；
//   - 标量 (scalar) 整体替换；
//   - 列表 (sequence) 整体替换 (例如 pipeline、award_ids、cron_times)。
//
// 合并完成后会重新套用默认值，因此区服可安全地只覆盖少量字段。
func (c *Config) EffectiveScheduler(serverID string) (SchedulerConfig, error) {
	override := c.findServerOverride(serverID)
	if override == nil || override.rawNode == nil {
		return c.Scheduler, nil
	}

	base, err := c.schedulerBaseNode()
	if err != nil {
		return SchedulerConfig{}, err
	}
	// 覆盖节点不应包含 server_overrides 自身，避免递归展开。
	removeMappingKey(base, "server_overrides")
	mergeYAMLNode(base, override.rawNode)

	var merged SchedulerConfig
	if err := base.Decode(&merged); err != nil {
		return SchedulerConfig{}, fmt.Errorf("decode merged scheduler for server %q: %w", serverID, err)
	}
	applySchedulerDefaults(&merged)
	return merged, nil
}

// findServerOverride 先精确匹配 server_id，未命中时按大小写不敏感回退。
func (c *Config) findServerOverride(serverID string) *ServerOverrideConfig {
	if c.Scheduler.ServerOverrides == nil {
		return nil
	}
	if ov, ok := c.Scheduler.ServerOverrides[serverID]; ok {
		return ov
	}
	for key, ov := range c.Scheduler.ServerOverrides {
		if strings.EqualFold(key, serverID) {
			return ov
		}
	}
	return nil
}

// schedulerBaseNode 返回 scheduler 段的原始配置节点副本。
// 若 Config 由 Load 构造，则使用保留的原始 YAML 节点；
// 手工构造的 Config 则退化为序列化当前 Scheduler 字段。
func (c *Config) schedulerBaseNode() (*yaml.Node, error) {
	if c.schedulerNode != nil {
		return cloneYAMLNode(c.schedulerNode), nil
	}

	base := c.Scheduler
	base.ServerOverrides = nil
	data, err := yaml.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("marshal scheduler base: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("unmarshal scheduler base: %w", err)
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0], nil
	}
	return &node, nil
}

// mergeYAMLNode 将 src 深度合并进 dst。
// dst 必须可写 (即来自 clone 或新构造的节点树)。
func mergeYAMLNode(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		*dst = *cloneYAMLNode(src)
		return
	}

	for i := 0; i+1 < len(src.Content); i += 2 {
		srcKey, srcVal := src.Content[i], src.Content[i+1]
		dstVal := findMappingValue(dst, srcKey.Value)
		if dstVal != nil {
			mergeYAMLNode(dstVal, srcVal)
			continue
		}
		dst.Content = append(dst.Content, cloneYAMLNode(srcKey), cloneYAMLNode(srcVal))
	}
}

func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// removeMappingKey 从映射节点中删除指定键 (原地修改)。
func removeMappingKey(node *yaml.Node, key string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	out := node.Content[:0]
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			continue
		}
		out = append(out, node.Content[i], node.Content[i+1])
	}
	node.Content = out
}

// cloneYAMLNode 深拷贝 YAML 节点树，避免跨区服合并时相互污染。
func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	if node.Alias != nil {
		cloned.Alias = cloneYAMLNode(node.Alias)
	}
	if len(node.Content) > 0 {
		cloned.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			cloned.Content[i] = cloneYAMLNode(child)
		}
	}
	return &cloned
}
