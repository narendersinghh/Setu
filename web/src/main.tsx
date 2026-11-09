import React from 'react'
import ReactDOM from 'react-dom/client'
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './styles/app.css'
import './styles/v2.css'
import { SessionProvider, useMeQuery } from './lib/session'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import { Login, Signup } from './pages/Auth'
import Team from './pages/Team'
import { Account, Guard, Modules, NotFound, Platform, Settings } from './pages/Core'
import { allRoutes } from './modules'

const qc = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, staleTime: 15_000, retry: 1 } } })

function Authed() {
  const me = useMeQuery()
  if (me.isLoading) return <Spinner />
  if (!me.data) return <Navigate to={'/login?next=' + encodeURIComponent(location.pathname + location.search)} replace />
  return <SessionProvider me={me.data}><Layout /></SessionProvider>
}

const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  { path: '/signup', element: <Signup /> },
  {
    path: '/', element: <Authed />, children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'account', element: <Account /> },
      { path: 'team', element: <Guard perm="team"><Team /></Guard> },
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
  <React.StrictMode><QueryClientProvider client={qc}><RouterProvider router={router} /></QueryClientProvider></React.StrictMode>,
)
