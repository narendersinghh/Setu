import { ComponentType } from 'react'
import { RouteObject } from 'react-router-dom'

/** A settings tab contributed by a module (shown in the Settings hub). */
export interface SettingsTab {
  key: string
  group: 'general' | 'channels' | 'prospecting' | 'crm' | 'system'
  label: string
  ic: string
  order?: number
  component: ComponentType
}

/** What every feature module exports from its index.tsx. */
export interface WebModule {
  routes?: RouteObject[]
  settings?: SettingsTab[]
}
