import React,{useEffect,useId,useRef} from "react";
import {useFocusTrap} from "../hooks/useFocusTrap";

export function Dialog({
  title,
  children,
  onClose,
  wide=false,
}:{
  title:string;
  children:React.ReactNode;
  onClose:()=>void;
  wide?:boolean;
}){
  const dialogRef=useRef<HTMLElement|null>(null);
  const titleId=useId();

  useFocusTrap(dialogRef,true,onClose);

  useEffect(()=>{
    document.body.classList.add("modal-open");
    return()=>document.body.classList.remove("modal-open");
  },[]);

  return <div
    className="dialog-backdrop"
    role="presentation"
    onMouseDown={event=>{if(event.currentTarget===event.target)onClose()}}
  >
    <section
      ref={dialogRef}
      className={"dialog "+(wide?"dialog-wide":"")}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      tabIndex={-1}
    >
      <div className="dialog-head">
        <h3 id={titleId}>{title}</h3>
        <button className="icon-button" onClick={onClose} aria-label="Close dialog">×</button>
      </div>
      {children}
    </section>
  </div>;
}
