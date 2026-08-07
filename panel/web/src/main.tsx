import { Component, StrictMode, type ErrorInfo, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { basePath } from './basePath'
import './styles.css'

class ErrorBoundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('UI crash', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div className="shell">
          <header className="topbar">
            <div className="brand">
              ha<span>panel</span>
            </div>
          </header>
          <p className="error">Ошибка интерфейса: {this.state.error.message}</p>
          <button
            className="btn btn-primary"
            type="button"
            onClick={() => window.location.reload()}
          >
            Обновить страницу
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter basename={basePath() || undefined}>
        <App />
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
)
