import { Component } from 'react'
import type { ErrorInfo, ReactNode } from 'react'
import BootErrorScreen from '../BootError'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

export default class AppErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('React render error:', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <BootErrorScreen
          error={this.state.error}
          title="화면을 불러오지 못했습니다"
          description="배포 중 화면 버전이 바뀌었거나 일시적인 브라우저 오류가 발생했습니다. 새로고침하면 최신 화면을 다시 불러옵니다."
        />
      )
    }
    return this.props.children
  }
}
