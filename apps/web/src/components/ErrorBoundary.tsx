import React from "react";

type State={hasError:boolean};

export class ErrorBoundary extends React.Component<{children:React.ReactNode},State>{
  state:State={hasError:false};

  static getDerivedStateFromError():State{
    return {hasError:true};
  }

  componentDidCatch(error:unknown){
    console.error("Nexora UI render failure",error);
  }

  render(){
    if(this.state.hasError){
      return <main className="fatal-error-shell">
        <section className="fatal-error-card" role="alert">
          <div className="fatal-error-mark" aria-hidden="true">!</div>
          <p className="eyebrow">NEXORA CONSOLE</p>
          <h1>Something went wrong</h1>
          <p>The console hit an unexpected rendering error. Your API credentials and server state were not changed.</p>
          <div className="fatal-error-actions">
            <button onClick={()=>this.setState({hasError:false})}>Try again</button>
            <button className="primary" onClick={()=>window.location.reload()}>Reload console</button>
          </div>
        </section>
      </main>;
    }
    return this.props.children;
  }
}
