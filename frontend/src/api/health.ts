export interface Health {
  status: string
}

export async function fetchHealth(signal?: AbortSignal): Promise<Health> {
  const res = await fetch('/api/healthz', { signal })
  if (!res.ok) throw new Error(`healthz: HTTP ${res.status}`)
  return (await res.json()) as Health
}
