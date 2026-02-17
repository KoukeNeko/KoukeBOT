import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { SettingsData } from "../../types";
import ToolResilienceSection from "./ToolResilienceSection";

function buildForm(): SettingsData {
  return {
    gemini_api_key: "",
    gemini_model: "gemini-2.5-pro",
    discord_token: "",
    provider_type: "oauth",
    provider_id: "google-gemini-cli",
    model_id: "google-gemini-cli/gemini-3-pro-preview",
    fallback_enabled: false,
    fallback_model_id: "",
    endpoint_policy: "primary_then_sandbox",
    brave_api_key: "",
    web_search_enabled: false,
    discord_use_reply: false,
    perplexity_api_key: "",
    search_provider: "auto",
    web_fetch_enabled: false,
    web_tool_mode: "classic",
    timezone: "auto",
    time_format: "auto",
    location: "",
    memory_extraction_enabled: false,
    memory_extraction_model: "",
    memory_recent_rounds: 50,
    log_channel_id: "",
    discord_persona_id: "",
    discord_observe_enabled: false,
    discord_member_intent_enabled: false,
    discord_presence_intent_enabled: false,
    discord_observe_max_results: 25,
    discord_observe_message_scope: "current_guild",
    tool_guard_enabled: false,
    max_tool_rounds: 5,
    max_web_search_calls_per_turn: 2,
    force_final_answer_after_tool_limit: true,
    transient_recovery_wait_enabled: true,
    transient_recovery_wait_max_seconds: 5,
    discord_show_internal_tool_errors: true,
    scheduled_monitor_max_per_user: 3,
    mcp: {
      enabled: false,
      verbose_logging: false,
      discovery_timeout_seconds: 10,
      tool_call_timeout_seconds: 20,
      servers: [],
    },
    persona_lock_every_turns: 4,
    triplet_output_enabled: true,
    analysis_visibility: "visible",
    negative_constraint_enabled: true,
    negative_constraint_default:
      "You are FORBIDDEN from switching into a generic assistant tone. Keep persona-specific stance, diction, and attitude.",
    fallback_model_enabled: true,
    fallback_model: "",
  };
}

describe("ToolResilienceSection", () => {
  it("stores fallback model id and legacy fallback model via dropdown", () => {
    const setForm = vi.fn();
    render(
      <ToolResilienceSection
        form={buildForm()}
        setForm={setForm}
        modelOptions={[
          {
            value: "google-gemini-cli/gemini-2.0-flash",
            label: "Google Gemini CLI / Gemini 2.0 Flash",
          },
        ]}
        saving={false}
        onSave={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByText("不使用備用模型", { selector: "span" }));
    fireEvent.click(screen.getByText("Google Gemini CLI / Gemini 2.0 Flash"));

    expect(setForm).toHaveBeenCalledWith(
      expect.objectContaining({
        fallback_model_id: "google-gemini-cli/gemini-2.0-flash",
        fallback_model: "gemini-2.0-flash",
      }),
    );
  });
});
