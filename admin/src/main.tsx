import React from 'react'
import ReactDOM from 'react-dom/client'
import App, { ApiDocsStandalone } from './App'
import './styles.css'

const path = window.location.pathname.replace(/\/$/, '')
const isStandaloneDocs = path === '/docs' || path === '/api-docs' || path === '/reference'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {isStandaloneDocs ? <ApiDocsStandalone /> : <App />}
  </React.StrictMode>
)
