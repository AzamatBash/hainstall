export type KbFieldId =
  | 'domain'
  | 'donor'
  | 'email'
  | 'realityPrivate'
  | 'realityPublic'
  | 'realityShortId'
  | 'panelIp'
  | 'nodeKey'

export type KbValues = Partial<Record<KbFieldId, string>>

export const KB_FIELD_META: {
  id: KbFieldId
  label: string
  placeholder: string
}[] = [
  { id: 'domain', label: 'Домен', placeholder: 'your.domain' },
  { id: 'donor', label: 'Домен-донор, SNI', placeholder: 'your-donor.de' },
  { id: 'email', label: 'E-mail', placeholder: 'you@example.com' },
  {
    id: 'realityPrivate',
    label: 'Reality privateKey',
    placeholder: 'из xray x25519',
  },
  {
    id: 'realityPublic',
    label: 'Reality publicKey',
    placeholder: 'из xray x25519',
  },
  {
    id: 'realityShortId',
    label: 'Reality shortId',
    placeholder: 'openssl rand -hex 8',
  },
  { id: 'panelIp', label: 'IP панели', placeholder: 'PANEL_IP' },
  { id: 'nodeKey', label: 'Ключ ноды', placeholder: 'из карточки ноды' },
]

const STORAGE_KEY = 'hapanel.kb.values'

export function loadKbValues(): KbValues {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as KbValues
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

export function saveKbValues(values: KbValues): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(values))
  } catch {
    /* ignore quota */
  }
}

export function clearKbValues(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* ignore */
  }
}

/** Apply filled placeholders. Empty fields leave the original tokens. */
export function applyKbPlaceholders(text: string, values: KbValues): string {
  let out = text
  const domain = (values.domain || '').trim()
  const donor = (values.donor || '').trim()
  const email = (values.email || '').trim()
  const priv = (values.realityPrivate || '').trim()
  const pub = (values.realityPublic || '').trim()
  const shortId = (values.realityShortId || '').trim()
  const panelIp = (values.panelIp || '').trim()
  const nodeKey = (values.nodeKey || '').trim()

  if (domain) {
    out = out.split('node1.your.domain').join(`node1.${domain}`)
    out = out.split('cdn.your.domain').join(`cdn.${domain}`)
    out = out.split('origin.your.domain').join(`origin.${domain}`)
    out = out.split('sub.your.domain').join(`sub.${domain}`)
    out = out.split('your.domain').join(domain)
  }
  if (donor) {
    out = out.split('your-donor.de').join(donor)
    out = out.split('your-donor.com').join(donor)
  }
  if (email) out = out.split('you@example.com').join(email)
  if (priv) {
    out = out.split('PRIVATE_KEY_X25519').join(priv)
    out = out.replace(/\bPRIVATE_KEY\b/g, priv)
  }
  if (pub) out = out.split('REALITY_PUBLIC_KEY').join(pub)
  if (shortId) out = out.split('REALITY_SHORT_ID').join(shortId)
  if (panelIp) out = out.split('PANEL_IP').join(panelIp)
  if (nodeKey) {
    out = out.split('КЛЮЧ_ИЗ_ПАНЕЛИ').join(nodeKey)
    // Value after SECRET_KEY= when it was the panel placeholder (already replaced)
    // Also common: SECRET_KEY= followed by empty or still the Cyrillic token.
  }
  return out
}
