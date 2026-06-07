import type { AccountPlatform, GroupPlatform, UpstreamProvider } from "@/types";

type Platform = AccountPlatform | GroupPlatform;

export interface UpstreamProviderMeta {
  value: UpstreamProvider;
  label: string;
  defaultPlatform: Platform;
  platforms: Platform[];
}

export const upstreamProviders: UpstreamProviderMeta[] = [
  { value: "", label: "Official", defaultPlatform: "anthropic", platforms: ["anthropic", "openai", "gemini", "antigravity"] },
  { value: "grok2api", label: "Grok2API", defaultPlatform: "grok2api", platforms: ["grok2api", "openai"] },
  { value: "windsurf", label: "WindsurfPoolAPI", defaultPlatform: "windsurf", platforms: ["windsurf", "openai", "anthropic"] },
  { value: "kiro", label: "Kiro-Go", defaultPlatform: "kiro", platforms: ["kiro", "anthropic", "openai"] },
];

export function providerForPlatform(platform?: string | null): UpstreamProvider {
  switch (platform) {
    case "grok2api":
      return "grok2api";
    case "windsurf":
      return "windsurf";
    case "kiro":
      return "kiro";
    default:
      return "";
  }
}

export function protocolForPlatform(platform?: string | null): Platform | string {
  switch (platform) {
    case "grok2api":
    case "windsurf":
      return "openai";
    case "kiro":
      return "anthropic";
    default:
      return platform || "";
  }
}

export function isOpenAIProtocolPlatform(platform?: string | null): boolean {
  return protocolForPlatform(platform) === "openai";
}

export function isAnthropicProtocolPlatform(platform?: string | null): boolean {
  return protocolForPlatform(platform) === "anthropic";
}

export function providerOptionsForPlatform(platform: Platform) {
  return upstreamProviders
    .filter((provider) => provider.platforms.includes(platform))
    .map((provider) => ({ value: provider.value, label: provider.label }));
}

export function normalizeUpstreamProvider(provider?: string | null): UpstreamProvider {
  const value = String(provider || "").trim().toLowerCase();
  if (value === "grok-2-api" || value === "grok2-api") return "grok2api";
  if (value === "windsurfpool" || value === "windsurfpoolapi" || value === "windsurf-pool-api") return "windsurf";
  if (value === "kirogo" || value === "kiro-go") return "kiro";
  if (value === "default" || value === "official") return "";
  return value as UpstreamProvider;
}

export function getUpstreamProviderLabel(provider?: string | null): string {
  const value = normalizeUpstreamProvider(provider);
  return upstreamProviders.find((item) => item.value === value)?.label || value || "Official";
}

export function getPlatformLabel(platform?: string | null): string {
  switch (platform) {
    case "anthropic":
      return "Anthropic";
    case "openai":
      return "OpenAI";
    case "gemini":
      return "Gemini";
    case "antigravity":
      return "Antigravity";
    case "grok2api":
      return "Grok2API";
    case "windsurf":
      return "WindsurfPool";
    case "kiro":
      return "Kiro-Go";
    default:
      return platform || "API";
  }
}

export function getUpstreamProviderClasses(provider?: string | null): string {
  switch (normalizeUpstreamProvider(provider)) {
    case "grok2api":
      return "bg-sky-50 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300";
    case "windsurf":
      return "bg-teal-50 text-teal-700 dark:bg-teal-900/30 dark:text-teal-300";
    case "kiro":
      return "bg-violet-50 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300";
    default:
      return "bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-gray-300";
  }
}
