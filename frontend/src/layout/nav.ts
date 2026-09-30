import type { Role } from '../api/types.ts'
import type { MessageKey } from '../i18n/index.ts'

export interface NavItem {
  to: string
  label: MessageKey
}

export interface NavSection {
  /** Visually hidden for the first (unnamed) section. */
  title?: MessageKey
  /** Roles that see the section; all logged-in users when omitted. */
  roles?: readonly Role[]
  items: readonly NavItem[]
}

export const navSections: readonly NavSection[] = [
  { items: [{ to: '/', label: 'nav.home' }] },
  {
    title: 'nav.management',
    roles: ['admin'],
    items: [
      { to: '/buildings', label: 'nav.buildings' },
      { to: '/room-types', label: 'nav.roomTypes' },
      { to: '/rooms', label: 'nav.rooms' },
      { to: '/groups', label: 'nav.groups' },
      { to: '/teachers', label: 'nav.teachers' },
      { to: '/disciplines', label: 'nav.disciplines' },
      { to: '/time-grid', label: 'nav.timeGrid' },
      { to: '/curriculum', label: 'nav.curriculum' },
    ],
  },
]
