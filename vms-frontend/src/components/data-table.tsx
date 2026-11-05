"use client";

import {useEffect,useId,useMemo,useState,type ReactNode} from "react";
import {getCoreRowModel,getFilteredRowModel,getPaginationRowModel,getSortedRowModel,useReactTable,type Column,type ColumnDef,type ColumnFiltersState,type FilterFn,type PaginationState,type SortingState,type VisibilityState} from "@tanstack/react-table";
import {ArrowDown,ArrowUp,ArrowUpDown,ChevronFirst,ChevronLast,ChevronLeft,ChevronRight,Filter,PlusCircle,Search,Settings2,X} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Label} from "@/components/ui/label";
import {Checkbox} from "@/components/ui/checkbox";
import {NativeSelect,NativeSelectOption} from "@/components/ui/native-select";
import {Popover,PopoverContent,PopoverTrigger} from "@/components/ui/popover";
import {DropdownMenu,DropdownMenuCheckboxItem,DropdownMenuContent,DropdownMenuLabel,DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {Table,TableBody,TableCell,TableHead,TableHeader,TableRow} from "@/components/ui/table";
import "./data-table.css";

type Value=string|number|null|undefined|string[];
type Option={value:string;label:string};
export type DataTableColumn<T>={
 id:string;header:string;accessor?:(row:T)=>Value;cell?:(row:T)=>ReactNode;
 filter?:"text"|"select"|"date"|"number"|false;options?:Option[];sortable?:boolean;hideable?:boolean;
};
type Props<T>={
 data:T[];columns:DataTableColumn<T>[];label:string;getRowId:(row:T)=>string;
 searchPlaceholder?:string;emptyMessage?:ReactNode;loading?:boolean;
 /** Cursor-backed histories expose only their loaded subset to local transforms. */
 partial?:boolean;hasMore?:boolean;onLoadMore?:()=>void|Promise<void>;loadingMore?:boolean;
};
const normalize=(value:unknown)=>String(value??"").normalize("NFD").replace(/[\u0300-\u036f]/g,"").toLocaleLowerCase("pt-BR");
function localDay(value:unknown){
 if(!value)return "";
 const date=new Date(String(value));
 if(Number.isNaN(date.getTime()))return "";
 return `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,"0")}-${String(date.getDate()).padStart(2,"0")}`;
}
function filterFor<T>(definition:DataTableColumn<T>):FilterFn<T>{
 return (row,id,value)=>{
  const raw=row.getValue<Value>(id);
  if(definition.filter==="select")return !value?.length||(Array.isArray(raw)?raw:[String(raw??"")]).some(v=>value.includes(v));
  if(definition.filter==="date"){
   const day=localDay(raw),[from,until]=value as string[];
   return (!from&&!until)||!!day&&(!from||day>=from)&&(!until||day<=until);
  }
  if(definition.filter==="number"){
   const [min,max]=value as string[];
   return (!min&&!max)||raw!==null&&raw!==undefined&&Number.isFinite(Number(raw))&&(!min||Number(raw)>=Number(min))&&(!max||Number(raw)<=Number(max));
  }
  return normalize(raw).includes(normalize(value));
 };
}

function ColumnFilter<T>({column,definition,options,onChange}:{column:Column<T>;definition:DataTableColumn<T>;options:Option[];onChange:(value:unknown)=>void}){
 const id=useId(),value=column.getFilterValue();
 const kind=definition.filter??"text";
 const selected=(value??[]) as string[];
 return <div className="data-table-filter-fields">
  {kind==="select"?<div className="data-table-options">{options.length?options.map(option=><Label className="data-table-option" key={option.value}><Checkbox checked={selected.includes(option.value)} onCheckedChange={checked=>onChange(checked?[...selected,option.value]:selected.filter(v=>v!==option.value))}/><span>{option.label}</span></Label>):<p>Ainda não há opções.</p>}</div>
   :kind==="date"||kind==="number"?<div className="data-table-range">{[0,1].map(index=><Label key={index} htmlFor={`${id}-${index}`}>{kind==="date"?(index?"Até":"De"):(index?"Máximo":"Mínimo")}<Input id={`${id}-${index}`} type={kind==="date"?"date":"number"} step={kind==="number"?"any":undefined} value={selected[index]??""} onChange={event=>{const next=[selected[0]??"",selected[1]??""];next[index]=event.target.value;onChange(next.some(Boolean)?next:undefined);}}/></Label>)}</div>
   :<Label htmlFor={id}>Contém<Input id={id} placeholder={`Filtrar ${definition.header.toLocaleLowerCase("pt-BR")}…`} value={String(value??"")} onChange={event=>onChange(event.target.value||undefined)}/></Label>}
  {column.getIsFiltered()&&<Button type="button" size="sm" variant="ghost" onClick={()=>onChange(undefined)}><X/>Limpar filtro</Button>}
 </div>;
}

export function DataTable<T>({data,columns,label,getRowId,searchPlaceholder="Buscar…",emptyMessage="Nenhum registro disponível.",loading=false,partial=false,hasMore=false,onLoadMore,loadingMore=false}:Props<T>){
 const [search,setSearch]=useState(""),[filters,setFilters]=useState<ColumnFiltersState>([]),[sorting,setSorting]=useState<SortingState>([]),[visibility,setVisibility]=useState<VisibilityState>({}),[pagination,setPagination]=useState<PaginationState>({pageIndex:0,pageSize:10});
 const searchId=useId(),sizeId=useId();
 const definitions=useMemo<ColumnDef<T>[]>(()=>columns.map(definition=>({
  id:definition.id,header:definition.header,accessorFn:definition.accessor,
  cell:context=>definition.cell?definition.cell(context.row.original):String(context.getValue()??"—"),
  enableSorting:!!definition.accessor&&definition.sortable!==false,
  enableColumnFilter:!!definition.accessor&&definition.filter!==false,
  enableGlobalFilter:!!definition.accessor,
  enableHiding:definition.hideable!==false,
  filterFn:filterFor(definition),
  sortingFn:(a,b,id)=>{
   const left=a.getValue<Value>(id),right=b.getValue<Value>(id);
   if(definition.filter==="date")return (left?new Date(String(left)).getTime():0)-(right?new Date(String(right)).getTime():0);
   if(typeof left==="number"&&typeof right==="number")return left-right;
   return String(left??"").localeCompare(String(right??""),"pt-BR",{numeric:true,sensitivity:"base"});
  },
 })),[columns]);
 const table=useReactTable({
  data,columns:definitions,getRowId,
  state:{globalFilter:search,columnFilters:filters,sorting,columnVisibility:visibility,pagination},
  onGlobalFilterChange:setSearch,onColumnFiltersChange:setFilters,onSortingChange:setSorting,onColumnVisibilityChange:setVisibility,onPaginationChange:setPagination,
  globalFilterFn:(row,id,value)=>normalize(row.getValue(id)).includes(normalize(value)),
  getColumnCanGlobalFilter:column=>!!column.accessorFn,
  getCoreRowModel:getCoreRowModel(),getFilteredRowModel:getFilteredRowModel(),getSortedRowModel:getSortedRowModel(),getPaginationRowModel:getPaginationRowModel(),
  autoResetPageIndex:false,
 });
 const count=table.getFilteredRowModel().rows.length,pageCount=Math.max(1,table.getPageCount());
 // Polling must preserve the current page, while removals clamp an obsolete page.
 useEffect(()=>{setPagination(current=>current.pageIndex>=pageCount?{...current,pageIndex:pageCount-1}:current);},[pageCount]);
 const resetPage=()=>setPagination(current=>({...current,pageIndex:0}));
 const changeFilter=(column:Column<T>,value:unknown)=>{column.setFilterValue(Array.isArray(value)&&!value.length?undefined:value);resetPage();};
 const optionsFor=(definition:DataTableColumn<T>)=>definition.options??Array.from(new Set(data.flatMap(row=>{const value=definition.accessor?.(row);return value===null||value===undefined?[]:Array.isArray(value)?value:[String(value)];}))).sort((a,b)=>a.localeCompare(b,"pt-BR",{numeric:true})).map(value=>({value,label:value}));
 const active=!!search||filters.length>0;
 const clear=()=>{setSearch("");setFilters([]);resetPage();};
 const visible=table.getVisibleLeafColumns();
 // Render row callbacks directly: replacing a column definition during polling
 // must not remount an open status popover or another stateful cell component.
 const renderCell=(row:T,id:string,value:unknown)=>{const definition=columns.find(column=>column.id===id);return definition?.cell?definition.cell(row):String(value??"—");};
 return <section className="data-table" aria-label={label} aria-busy={loading}>
  <div className="data-table-toolbar">
   <div className="data-table-search"><Search aria-hidden="true"/><Label className="sr-only" htmlFor={searchId}>{`Buscar em ${label}`}</Label><Input id={searchId} value={search} placeholder={searchPlaceholder} onChange={event=>{setSearch(event.target.value);resetPage();}}/></div>
   {columns.filter(definition=>definition.filter==="select").map(definition=>{
    const column=table.getColumn(definition.id)!;const selected=(column.getFilterValue()??[]) as string[];
    return <Popover key={definition.id}><PopoverTrigger asChild><Button type="button" variant="outline" size="sm" className="data-table-facet" aria-label={`Filtrar ${definition.header}`}><PlusCircle aria-hidden="true"/>{definition.header}{selected.length>0&&<span className="data-table-filter-count">{selected.length}</span>}</Button></PopoverTrigger><PopoverContent className="data-table-popover" align="start" aria-label={`Filtro de ${definition.header}`}><strong>{definition.header}</strong><ColumnFilter column={column} definition={definition} options={optionsFor(definition)} onChange={value=>changeFilter(column,value)}/></PopoverContent></Popover>;
   })}
   {active&&<Button type="button" variant="ghost" size="sm" onClick={clear}>Limpar filtros<X aria-hidden="true"/></Button>}
   <DropdownMenu><DropdownMenuTrigger asChild><Button type="button" variant="outline" size="sm" className="data-table-columns"><Settings2 aria-hidden="true"/>Colunas</Button></DropdownMenuTrigger><DropdownMenuContent align="end" className="min-w-44"><DropdownMenuLabel>Colunas visíveis</DropdownMenuLabel>{columns.map(definition=>{const column=table.getColumn(definition.id)!;return <DropdownMenuCheckboxItem key={definition.id} checked={column.getIsVisible()} disabled={!column.getCanHide()||column.getIsVisible()&&visible.length===1} onSelect={event=>event.preventDefault()} onCheckedChange={checked=>column.toggleVisibility(checked)}>{definition.header}</DropdownMenuCheckboxItem>;})}</DropdownMenuContent></DropdownMenu>
  </div>
  {filters.length>0&&<div className="data-table-active-filters" aria-label="Filtros aplicados">{filters.map(filter=>{const definition=columns.find(c=>c.id===filter.id)!;const values=Array.isArray(filter.value)?filter.value.map(v=>definition.options?.find(o=>o.value===v)?.label??v).filter(Boolean).join(" · "):String(filter.value);return <Button type="button" size="sm" variant="secondary" key={filter.id} onClick={()=>changeFilter(table.getColumn(filter.id)!,undefined)} aria-label={`Remover filtro ${definition.header}`}><span>{definition.header}: {values}</span><X aria-hidden="true"/></Button>;})}</div>}
  {partial&&<p className="data-table-scope">Busca, filtros e ordenação abrangem os {data.length} registros carregados.{hasMore?" Carregue os anteriores para ampliar a consulta.":""}</p>}
  <div className="data-table-frame"><Table aria-label={label}>
   <TableHeader>{table.getHeaderGroups().map(group=><TableRow key={group.id}>{group.headers.map(header=>{
    const column=header.column,definition=columns.find(c=>c.id===column.id)!;const sorted=column.getIsSorted();
    return <TableHead key={header.id} scope="col" aria-sort={sorted==="asc"?"ascending":sorted==="desc"?"descending":"none"}>
     {column.getCanSort()||column.getCanFilter()?<Popover><PopoverTrigger asChild><Button type="button" variant="ghost" size="sm" className="data-table-heading" aria-label={`Opções da coluna ${definition.header}`}>{definition.header}{sorted==="asc"?<ArrowUp aria-hidden="true"/>:sorted==="desc"?<ArrowDown aria-hidden="true"/>:<ArrowUpDown aria-hidden="true"/>}{column.getIsFiltered()&&<Filter className="data-table-filtered" aria-hidden="true"/>}</Button></PopoverTrigger><PopoverContent className="data-table-popover" align="start" aria-label={`Opções de ${definition.header}`}><strong>{definition.header}</strong>{column.getCanSort()&&<div className="data-table-sort-actions"><Button type="button" variant={sorted==="asc"?"secondary":"ghost"} size="sm" onClick={()=>{column.toggleSorting(false);resetPage();}}><ArrowUp/>Ordem crescente</Button><Button type="button" variant={sorted==="desc"?"secondary":"ghost"} size="sm" onClick={()=>{column.toggleSorting(true);resetPage();}}><ArrowDown/>Ordem decrescente</Button>{sorted&&<Button type="button" variant="ghost" size="sm" onClick={()=>{column.clearSorting();resetPage();}}>Remover ordenação</Button>}</div>}{column.getCanFilter()&&<ColumnFilter column={column} definition={definition} options={optionsFor(definition)} onChange={value=>changeFilter(column,value)}/>}</PopoverContent></Popover>:definition.header}
    </TableHead>;
   })}</TableRow>)}</TableHeader>
   <TableBody>{loading?<TableRow><TableCell colSpan={visible.length} className="data-table-empty" role="status">Carregando registros…</TableCell></TableRow>:table.getRowModel().rows.length?table.getRowModel().rows.map(row=><TableRow key={row.id}>{row.getVisibleCells().map(cell=><TableCell key={cell.id}>{renderCell(row.original,cell.column.id,cell.getValue())}</TableCell>)}</TableRow>):<TableRow><TableCell colSpan={visible.length} className="data-table-empty">{active?<><p>Nenhum resultado para os filtros aplicados.</p><Button type="button" variant="ghost" onClick={clear}>Limpar filtros</Button></>:emptyMessage}</TableCell></TableRow>}</TableBody>
  </Table></div>
  <div className="data-table-footer">
   <p role="status" aria-live="polite">{count?`${pagination.pageIndex*pagination.pageSize+1}–${Math.min((pagination.pageIndex+1)*pagination.pageSize,count)} de ${count}`:"0"} {partial?"registros carregados":"registros"}{active?` (de ${data.length})`:""}</p>
   <div className="data-table-page-size"><Label htmlFor={sizeId}>Linhas por página</Label><NativeSelect id={sizeId} value={pagination.pageSize} onChange={event=>setPagination({pageIndex:0,pageSize:Number(event.target.value)})}>{[10,20,30,50,100].map(size=><NativeSelectOption key={size} value={size}>{size}</NativeSelectOption>)}</NativeSelect></div>
   <span className="data-table-page">Página {Math.min(pagination.pageIndex+1,pageCount)} de {pageCount}{partial?" carregadas":""}</span>
   <nav className="data-table-navigation" aria-label={`Paginação de ${label}`}>
    {!partial&&<Button type="button" size="icon-sm" variant="outline" aria-label="Primeira página" disabled={!table.getCanPreviousPage()} onClick={()=>table.firstPage()}><ChevronFirst/></Button>}
    <Button type="button" size="icon-sm" variant="outline" aria-label="Página anterior" disabled={!table.getCanPreviousPage()} onClick={()=>table.previousPage()}><ChevronLeft/></Button>
    <Button type="button" size="icon-sm" variant="outline" aria-label="Próxima página" disabled={!table.getCanNextPage()} onClick={()=>table.nextPage()}><ChevronRight/></Button>
    {!partial&&<Button type="button" size="icon-sm" variant="outline" aria-label="Última página" disabled={!table.getCanNextPage()} onClick={()=>table.lastPage()}><ChevronLast/></Button>}
   </nav>
  </div>
  {hasMore&&onLoadMore&&<div className="data-table-load-more"><Button type="button" variant="outline" disabled={loadingMore} onClick={()=>void onLoadMore()}>{loadingMore?"Carregando anteriores…":"Carregar registros anteriores"}</Button></div>}
 </section>;
}
