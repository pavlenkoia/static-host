export type AgentKey = { id: string; name: string; created_at: number; expires_at: number|null; revoked_at: number|null; status: 'active'|'expired'|'revoked'; can_reveal: boolean }
