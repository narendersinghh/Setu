// Registry of feature modules (web). Each module exports { routes, settings } from its index.tsx.
import { RouteObject } from 'react-router-dom'
import { SettingsTab, WebModule } from './types'
import crm from './crm'
import capture from './capture'
import automation from './automation'
import booking from './booking'
import messaging from './messaging'
import prospecting from './prospecting'
import reports from './reports'
import usage from './usage'
import ai from './ai'
import billing from './billing'

const mods: WebModule[] = [crm, capture, automation, booking, messaging, prospecting, reports, usage, ai, billing]

export const allRoutes: RouteObject[] = mods.flatMap(m => m.routes || [])
export const allSettingsTabs: SettingsTab[] = mods.flatMap(m => m.settings || [])
