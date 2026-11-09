import React from 'react'
import ReactDOM from 'react-dom/client'
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './styles/tokens.css'
import './styles/app.css'
import './styles/v2.css'
import { SessionProvider, useMeQuery, Me } from './lib/session'
import { ThemeProvider } from './lib/theme'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import { Forgot, Login, Reset, Signup } from './pages/Auth'
import Team from './pages/Team'
import { Account, Guard, Modules, NotFound, Platform, Settings } from './pages/Core'
import PlatformApp from '@/platform'
import { allRoutes } from './modules'

const qc = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, staleTime: 15_000, retry: 1 } } })

function Authed() {
  const me = useMeQuery()
  if (me.isLoading) return <Spinner />
  if (!me.data) return <Navigate to={'/login?next=' + encodeURIComponent(location.pathname + location.search)} replace />
  // platform-console session (platform admin, no organisation)
  if (!me.data.org) return <Navigate to="/admin" replace />
  return <SessionProvider me={me.data as Me}><Layout /></SessionProvider>
}

const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  { path: '/signup', element: <Signup /> },
  { path: '/forgot', element: <Forgot /> },
  { path: '/reset', element: <Reset /> },
  { path: '/admin/*', element: <PlatformApp /> },
  {
    path: '/', element: <Authed />, children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'account', element: <Account /> },
      { path: 'team', element: <Guard perm="team"><Team /></Guard> },
      // modules are platform-admin only in v2 (route kept, no menu item)
      { path: 'modules', element: <Guard perm="modules"><Modules /></Guard> },
      { path: 'settings', element: <Guard perm="settings"><Settings /></Guard> },
      { path: 'settings/:tab', element: <Guard perm="settings"><Settings /></Guard> },
      { path: 'platform', element: <Platform /> },
      ...allRoutes,
      { path: '*', element: <NotFound /> },
    ],
  },
])

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><ThemeProvider><QueryClientProvider client={qc}><RouterProvider router={router} /></QueryClientProvider></ThemeProvider></React.StrictMode>,
)
