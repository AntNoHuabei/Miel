package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const (
	AgentProfileWork   = "work"
	AgentProfileCoding = "coding"
)

type ProfileModel struct {
	ProviderID int64  `json:"providerId"`
	Model      string `json:"model"`
}

type AgentProfileDefaults struct {
	Work   ProfileModel `json:"work"`
	Coding ProfileModel `json:"coding"`
}

type SetAgentProfileDefaultRequest struct {
	Profile    string `json:"profile"`
	ProviderID int64  `json:"providerId"`
	Model      string `json:"model"`
}

type SetConversationProfileRequest struct {
	ConversationID int64  `json:"conversationId"`
	Profile        string `json:"profile"`
}

func normalizeAgentProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", AgentProfileWork:
		return AgentProfileWork
	case AgentProfileCoding:
		return AgentProfileCoding
	default:
		return ""
	}
}

func (s *SettingsService) profileDefault(profile string) (ProfileModel, error) {
	profile = normalizeAgentProfile(profile)
	if profile == "" {
		return ProfileModel{}, errors.New("不支持的 Agent 模式")
	}
	var result ProfileModel
	err := s.db.QueryRow(`SELECT provider_id, model FROM agent_profile_defaults WHERE profile = ?`, profile).
		Scan(&result.ProviderID, &result.Model)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProfileModel{}, err
	}
	provider, err := s.DefaultProvider()
	if err != nil {
		return ProfileModel{}, err
	}
	result = ProfileModel{ProviderID: provider.ID, Model: provider.Model}
	_, err = s.db.Exec(`INSERT INTO agent_profile_defaults (profile, provider_id, model) VALUES (?, ?, ?)`, profile, result.ProviderID, result.Model)
	return result, err
}

func (s *SettingsService) GetAgentProfileDefaults() (AgentProfileDefaults, error) {
	work, err := s.profileDefault(AgentProfileWork)
	if err != nil {
		return AgentProfileDefaults{}, err
	}
	coding, err := s.profileDefault(AgentProfileCoding)
	if err != nil {
		return AgentProfileDefaults{}, err
	}
	return AgentProfileDefaults{Work: work, Coding: coding}, nil
}

func (s *SettingsService) SetAgentProfileDefault(req SetAgentProfileDefaultRequest) error {
	profile := normalizeAgentProfile(req.Profile)
	if profile == "" {
		return errors.New("不支持的 Agent 模式")
	}
	provider, err := configuredProvider(req.ProviderID, req.Model)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO agent_profile_defaults (profile, provider_id, model) VALUES (?, ?, ?)
		ON CONFLICT(profile) DO UPDATE SET provider_id = excluded.provider_id, model = excluded.model`,
		profile, provider.ID, provider.Model)
	if err == nil {
		s.notifyModelsChanged()
	}
	return err
}

func profileModelForConversation(conversationID int64, profile string) (ProfileModel, error) {
	profile = normalizeAgentProfile(profile)
	if profile == "" {
		return ProfileModel{}, errors.New("不支持的 Agent 模式")
	}
	if conversationID <= 0 {
		return settingsSvc.profileDefault(profile)
	}
	var result ProfileModel
	err := store.QueryRow(`SELECT provider_id, model FROM conversation_profile_models WHERE conversation_id = ? AND profile = ?`, conversationID, profile).
		Scan(&result.ProviderID, &result.Model)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProfileModel{}, err
	}
	result, err = settingsSvc.profileDefault(profile)
	if err != nil {
		return ProfileModel{}, err
	}
	_, err = store.Exec(`INSERT INTO conversation_profile_models (conversation_id, profile, provider_id, model) VALUES (?, ?, ?, ?)`, conversationID, profile, result.ProviderID, result.Model)
	return result, err
}

func providerForConversationProfile(conversationID int64, profile string) (Provider, error) {
	selected, err := profileModelForConversation(conversationID, profile)
	if err != nil {
		return Provider{}, err
	}
	return configuredProvider(selected.ProviderID, selected.Model)
}

func initializeConversationProfiles(ctx context.Context, tx *sql.Tx, conversationID int64, activeProfile string, active Provider) error {
	for _, profile := range []string{AgentProfileWork, AgentProfileCoding} {
		selected := ProfileModel{ProviderID: active.ID, Model: active.Model}
		configured, err := profileDefaultInTransaction(ctx, tx, profile)
		if err == nil {
			selected = configured
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if profile == activeProfile && active.ID > 0 {
			selected = ProfileModel{ProviderID: active.ID, Model: active.Model}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO conversation_profile_models (conversation_id, profile, provider_id, model) VALUES (?, ?, ?, ?)`, conversationID, profile, selected.ProviderID, selected.Model); err != nil {
			return err
		}
	}
	return nil
}

func profileDefaultInTransaction(ctx context.Context, tx *sql.Tx, profile string) (ProfileModel, error) {
	var result ProfileModel
	err := tx.QueryRowContext(ctx, `SELECT provider_id, model FROM agent_profile_defaults WHERE profile = ?`, profile).
		Scan(&result.ProviderID, &result.Model)
	return result, err
}

func (s *AgentService) SetConversationProfile(req SetConversationProfileRequest) error {
	profile := normalizeAgentProfile(req.Profile)
	if req.ConversationID <= 0 || profile == "" {
		return errors.New("会话或 Agent 模式无效")
	}
	if !s.beginConversationRun(req.ConversationID) {
		return errConversationBusy
	}
	defer s.finishConversationRun(req.ConversationID)
	selected, err := profileModelForConversation(req.ConversationID, profile)
	if err != nil {
		return err
	}
	if _, err := configuredProvider(selected.ProviderID, selected.Model); err != nil {
		return err
	}
	result, err := store.Exec(`UPDATE conversations SET agent_profile = ?, provider_id = ?, model = ?, updated_at = ? WHERE id = ?`, profile, selected.ProviderID, selected.Model, now(), req.ConversationID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("会话不存在")
	}
	s.emit("conversations.changed", "updated")
	return nil
}
