import React from 'react'
import ReactDOM from 'react-dom/client'
import ThemeProvider from './theme/ThemeContext'
import App from './App'
import './index.css'
import './styles/todos.css'
import './styles/quick-assistant.css'
import './styles/app-shell.css'
import './styles/settings.css'
import './styles/setup-wizard.css'

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider>
      <App />
    </ThemeProvider>
  </React.StrictMode>,
)
