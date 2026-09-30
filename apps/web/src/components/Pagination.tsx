import {useMemo} from "react";

function buildPageList(current:number,total:number):(number|"…")[]{
  if(total<=7)return Array.from({length:total},(_,i)=>i+1);
  const pages:(number|"…")[]=[1];
  if(current>3)pages.push("…");
  for(let p=Math.max(2,current-1);p<=Math.min(total-1,current+1);p++)pages.push(p);
  if(current<total-2)pages.push("…");
  pages.push(total);
  return pages;
}

export function Pagination({page,pageSize,totalItems,onPageChange}:{
  page:number;
  pageSize:number;
  totalItems:number;
  onPageChange:(page:number)=>void;
}){
  const pageCount=Math.max(1,Math.ceil(totalItems/pageSize));
  const pages=useMemo(()=>buildPageList(page,pageCount),[page,pageCount]);
  if(pageCount<=1)return null;

  const from=(page-1)*pageSize+1;
  const to=Math.min(page*pageSize,totalItems);

  return <nav className="pagination" aria-label="Pagination">
    <span className="pagination-summary">Showing {from}–{to} of {totalItems}</span>
    <div className="pagination-controls">
      <button
        type="button"
        className="pagination-btn"
        onClick={()=>onPageChange(page-1)}
        disabled={page<=1}
        aria-label="Previous page"
      >‹</button>
      {pages.map((p,i)=>p==="…"
        ?<span key={"gap-"+i} className="pagination-ellipsis" aria-hidden="true">…</span>
        :<button
            key={p}
            type="button"
            className={"pagination-btn "+(p===page?"active":"")}
            onClick={()=>onPageChange(p)}
            aria-current={p===page?"page":undefined}
          >{p}</button>
      )}
      <button
        type="button"
        className="pagination-btn"
        onClick={()=>onPageChange(page+1)}
        disabled={page>=pageCount}
        aria-label="Next page"
      >›</button>
    </div>
  </nav>;
}
