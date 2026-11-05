import React,{useState} from "react";
import {createRoot} from "react-dom/client";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import {Button} from "@/components/ui/button";
import {IntegrationFixture} from "./integration";

type Row={id:string;name:string;status:string;createdAt:string;quantity:number};
const rows:Row[]=Array.from({length:37},(_,index)=>({
 id:String(index+1),name:`Câmera ${String(index+1).padStart(2,"0")}`,
 status:index%3===0?"Desativada":"Habilitada",
 createdAt:`2026-10-${String(index%31+1).padStart(2,"0")}T12:00:00-03:00`,quantity:index+1,
}));
const columns:DataTableColumn<Row>[]=[
 {id:"name",header:"Câmera",accessor:row=>row.name,hideable:false},
 {id:"status",header:"Operação",accessor:row=>row.status,filter:"select",options:[{value:"Habilitada",label:"Habilitada"},{value:"Desativada",label:"Desativada"}]},
 {id:"createdAt",header:"Cadastro",accessor:row=>row.createdAt,filter:"date",cell:row=>new Date(row.createdAt).toLocaleDateString("pt-BR")},
 {id:"quantity",header:"Quantidade",accessor:row=>row.quantity,filter:"number"},
 {id:"actions",header:"Ações",filter:false,sortable:false,cell:row=><Button variant="ghost" onClick={()=>window.alert(`Registro ${row.id}`)}>Detalhes</Button>},
];
function Fixture(){
 const [data,setData]=useState(rows),[refresh,setRefresh]=useState(0),[partial,setPartial]=useState(rows.slice(0,12)),[loading,setLoading]=useState(false);
 return <main className="astra-interface" style={{maxWidth:1200,margin:"32px auto",padding:20}}>
  <h1 style={{fontSize:24,fontWeight:600,marginBottom:12}}>Validação sintética das tabelas</h1>
  <p style={{marginBottom:16}}>37 câmeras fictícias, sem conexão com a instalação.</p>
  <div style={{display:"flex",flexWrap:"wrap",gap:8,marginBottom:16}}>
   <Button variant="outline" onClick={()=>{setData(previous=>previous.map(row=>({...row})));setRefresh(value=>value+1);}}>Simular polling</Button>
   <Button variant="outline" onClick={()=>setData(rows.slice(0,3))}>Reduzir para 3 registros</Button>
   <Button variant="outline" onClick={()=>setData(rows)}>Restaurar 37 registros</Button>
   <Button variant="outline" onClick={()=>setLoading(value=>!value)}>Alternar carregamento</Button>
   <span role="status">Atualizações: {refresh}</span>
  </div>
  <DataTable label="Câmeras sintéticas" data={data} columns={columns} getRowId={row=>row.id} loading={loading}/>
  <h2 style={{fontSize:20,margin:"40px 0 12px"}}>Histórico com cursor simulado</h2>
  <DataTable label="Histórico sintético" data={partial} columns={columns} getRowId={row=>row.id} partial hasMore={partial.length<rows.length} onLoadMore={()=>setPartial(previous=>rows.slice(0,Math.min(previous.length+12,rows.length)))}/>
  <h2 style={{fontSize:20,margin:"40px 0 12px"}}>Conjunto vazio</h2>
  <DataTable label="Tabela vazia" data={[]} columns={columns} getRowId={row=>row.id} emptyMessage="Nenhuma câmera sintética cadastrada."/>
 </main>;
}
createRoot(document.getElementById("root")!).render(location.search.includes("integration")?<IntegrationFixture/>:<Fixture/>);
