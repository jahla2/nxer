import {RefObject,useEffect} from "react";

const FOCUSABLE_SELECTOR=[
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

export function useFocusTrap(
  containerRef:RefObject<HTMLElement|null>,
  active:boolean,
  onEscape?:()=>void,
){
  useEffect(()=>{
    if(!active)return;

    const previous=document.activeElement instanceof HTMLElement
      ?document.activeElement
      :null;
    const container=containerRef.current;
    if(!container)return;

    const focusables=()=>Array.from(
      container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    ).filter(element=>
      !element.hasAttribute("hidden")
      &&element.getAttribute("aria-hidden")!=="true"
      &&element.offsetParent!==null,
    );

    const initial=
      container.querySelector<HTMLElement>("[data-autofocus]")
      ??focusables()[0]
      ??container;
    window.setTimeout(()=>initial.focus(),0);

    function onKeyDown(event:KeyboardEvent){
      if(event.key==="Escape"&&onEscape){
        event.preventDefault();
        onEscape();
        return;
      }
      if(event.key!=="Tab")return;

      const items=focusables();
      if(items.length===0){
        event.preventDefault();
        container.focus();
        return;
      }

      const first=items[0];
      const last=items[items.length-1];
      if(event.shiftKey&&document.activeElement===first){
        event.preventDefault();
        last.focus();
      }else if(!event.shiftKey&&document.activeElement===last){
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown",onKeyDown);
    return()=>{
      document.removeEventListener("keydown",onKeyDown);
      if(previous&&document.contains(previous)){
        window.setTimeout(()=>previous.focus(),0);
      }
    };
  },[active,containerRef,onEscape]);
}
