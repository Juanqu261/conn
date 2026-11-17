export const analyzerCapability = {
  language: "typescript",
  status: "scaffold",
} as const;

export type AnalyzerCapability = typeof analyzerCapability;
