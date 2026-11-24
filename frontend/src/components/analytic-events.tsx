"use client";

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { CalendarDays, ChevronLeft, ChevronRight, PlusCircle, RefreshCw, X } from "lucide-react";
import { EditorDialog } from "@/components/form-dialog";
import { EventFailures } from "@/components/event-collection";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { api, ApiError } from "@/lib/api";
import { onEnvironmentChange } from "@/lib/events";
import "./analytic-events.css";

const typeNames = { intrusion: "Intrusão", line_crossing: "Cruzamento de linha" };
type EventType = keyof typeof typeNames;
type AnalyticEvent = {
  id: string;
  type: EventType;
  cameraId: string;
  cameraName: string;
  siteId: string;
  siteName: string;
  ruleName?: string;
  ruleId?: string;
  state?: string;
  receivedAt: string;
  cameraAt?: string;
  simulated: boolean;
};
type Option = { id: string; name: string };
type FilterOptions = { sites: Option[]; cameras: Option[] };
type Period = { from: string; until: string };
type Facet = "type" | "site" | "camera";
type Filters = { period: Period | null } & Record<Facet, string[]>;
type EventsPage = Period & { items: AnalyticEvent[]; nextCursor: string };
type ListQuery = { filters: Filters; pageIndex: number; pageSize: number; cursors: string[]; window?: Period };
type ListResult = { query: ListQuery; page?: EventsPage; error?: Error };

const emptyFilters: Filters = { period: null, type: [], site: [], camera: [] };
const typeOptions = Object.entries(typeNames).map(([id, name]) => ({ id, name }));
const filterOptions: FilterOptions = { sites: [], cameras: [] };
const dateFormat = new Intl.DateTimeFormat("pt-BR", { dateStyle: "short", timeStyle: "medium" });

function pageParams(query: ListQuery) {
  const params = new URLSearchParams({ limit: String(query.pageSize) });
  const period = query.window ?? query.filters.period;
  if (period) {
    params.set("from", period.from);
    params.set("until", period.until);
  }
  const cursor = query.cursors[query.pageIndex];
  if (cursor) params.set("cursor", cursor);
  for (const facet of ["type", "site", "camera"] as const) {
    for (const id of query.filters[facet]) params.append(facet, id);
  }
  return params;
}

function localInputValue(timestamp: string) {
  const date = new Date(timestamp);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 19);
}

function PeriodFilter({ selected, visiblePeriod, onChange }: { selected: Period | null; visiblePeriod?: Period; onChange: (value: Period | null) => void }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [from, setFrom] = useState("");
  const [until, setUntil] = useState("");
  const [error, setError] = useState("");
  function openChange(next: boolean) {
    if (next) {
      const now = Date.now();
      const period = selected ?? visiblePeriod ?? { from: new Date(now - 86_400_000).toISOString(), until: new Date(now).toISOString() };
      setFrom(localInputValue(period.from));
      setUntil(localInputValue(period.until));
      setError("");
    }
    setOpen(next);
  }
  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const start = new Date(from), end = new Date(until);
    if (!(start.getTime() < end.getTime())) {
      setError("A data final deve ser posterior à inicial.");
      return;
    }
    onChange({ from: start.toISOString(), until: end.toISOString() });
    setOpen(false);
  }
  return <Popover open={open} onOpenChange={openChange}>
    <PopoverTrigger asChild>
      <Button variant={selected ? "secondary" : "outline"} size="sm" aria-label={selected ? "Período personalizado" : "Período: últimas 24 horas"}>
        <CalendarDays aria-hidden="true" />{selected ? "Período personalizado" : "Últimas 24 horas"}
      </Button>
    </PopoverTrigger>
    <PopoverContent align="start" className="w-[min(20rem,calc(100vw-2rem))] gap-3 p-4" aria-label="Filtro de período">
      <PopoverTitle>Período</PopoverTitle>
      <form className="grid gap-3" onSubmit={apply}>
        <div className="grid gap-2"><Label htmlFor={`${id}-from`}>De</Label>
          <Input id={`${id}-from`} type="datetime-local" step={1} className="w-full min-w-0" value={from} onChange={event => setFrom(event.target.value)} required />
        </div>
        <div className="grid gap-2"><Label htmlFor={`${id}-until`}>Até</Label>
          <Input id={`${id}-until`} type="datetime-local" step={1} className="w-full min-w-0" value={until} onChange={event => setUntil(event.target.value)} required />
        </div>
        <p className="text-muted-foreground">Fuso {dateFormat.resolvedOptions().timeZone}</p>
        {error && <p className="text-destructive" role="alert">{error}</p>}
        <div className="flex flex-wrap justify-end gap-2">
          {selected && <Button variant="ghost" size="sm" type="button" onClick={() => { onChange(null); setOpen(false); }}>Últimas 24 horas</Button>}
          <Button size="sm" type="submit">Aplicar</Button>
        </div>
      </form>
    </PopoverContent>
  </Popover>;
}

function MultiFilter({ label, options, selected, onChange }: { label: string; options: Option[]; selected: string[]; onChange: (ids: string[]) => void }) {
  const id = useId();
  const [search, setSearch] = useState("");
  const normalize = (value: string) => value.toLocaleLowerCase("pt-BR").normalize("NFD").replace(/\p{Diacritic}/gu, "");
  const visible = options.filter(option => normalize(option.name).includes(normalize(search)));
  return <Popover onOpenChange={open => { if (!open) setSearch(""); }}>
    <PopoverTrigger asChild>
      <Button variant={selected.length ? "secondary" : "outline"} size="sm" aria-label={`Filtrar ${label}${selected.length ? `, ${selected.length} selecionados` : ""}`}>
        <PlusCircle aria-hidden="true" />{label}{selected.length > 0 && <span className="data-table-filter-count">{selected.length}</span>}
      </Button>
    </PopoverTrigger>
    <PopoverContent align="start" className="data-table-popover" aria-label={`Filtro de ${label}`}>
      <PopoverTitle>{label}</PopoverTitle>
      {options.length > 10 && <div>
        <Label htmlFor={id} className="sr-only">Buscar em {label}</Label>
        <Input id={id} value={search} onChange={event => setSearch(event.target.value)} placeholder="Buscar opções…" />
      </div>}
      <div className="data-table-options">
        {visible.map(option => <Label className="data-table-option" key={option.id}>
          <Checkbox checked={selected.includes(option.id)} onCheckedChange={checked => onChange(checked === true ? [...selected, option.id] : selected.filter(value => value !== option.id))} />
          <span className="min-w-0 break-words">{option.name}</span>
        </Label>)}
        {!visible.length && <p className="text-muted-foreground">{search ? "Nenhuma opção encontrada." : "Nenhuma opção disponível."}</p>}
      </div>
      {selected.length > 0 && <Button variant="ghost" size="sm" onClick={() => onChange([])}><X aria-hidden="true" />Limpar filtro</Button>}
    </PopoverContent>
  </Popover>;
}

function EventDetails({ event, onClose, returnFocus, fallbackFocus }: { event: AnalyticEvent; onClose: () => void; returnFocus: HTMLElement | null; fallbackFocus: () => HTMLElement | null }) {
  return <EditorDialog title={typeNames[event.type]} onClose={onClose} returnFocus={returnFocus} fallbackFocus={fallbackFocus} wide className="event-details-dialog">
    <ScrollArea>
      <div className="p-4 sm:p-6">
        <div className="mb-4 aspect-video w-full rounded-xl border bg-muted" data-event-image aria-hidden="true" />
        {event.simulated && <Badge variant="secondary" className="mb-4">Simulado</Badge>}
        <dl className="grid gap-4 text-sm sm:grid-cols-2 [&_dd]:mt-1 [&_dd]:break-words [&_dt]:text-xs [&_dt]:text-muted-foreground">
          <div><dt>Câmera</dt><dd>{event.cameraName} <span className="text-muted-foreground">#{event.cameraId}</span></dd></div>
          <div><dt>Site</dt><dd>{event.siteName}</dd></div>
          <div><dt>Recebido no Astra</dt><dd>{dateFormat.format(new Date(event.receivedAt))}</dd></div>
          {event.cameraAt && <div><dt>Horário da câmera</dt><dd>{dateFormat.format(new Date(event.cameraAt))}</dd></div>}
          {(event.ruleName || event.ruleId) && <div><dt>{event.type === "intrusion" ? "Área" : "Linha"}</dt><dd>{event.ruleName || "#" + event.ruleId}</dd></div>}
          {event.state && <div><dt>Estado do alerta</dt><dd>{event.state === "inactive" ? "Encerramento" : "Ativo"}</dd></div>}
        </dl>
      </div>
    </ScrollArea>
  </EditorDialog>;
}

export function AnalyticEventsWorkspace({ tenantId }: { tenantId: string }) {
  const base = `/tenants/${tenantId}/analytic-events`;
  const [query, setQuery] = useState<ListQuery>({ filters: emptyFilters, pageIndex: 0, pageSize: 30, cursors: [""] });
  const [result, setResult] = useState<ListResult | null>(null);
  const [options, setOptions] = useState<FilterOptions>(filterOptions);
  const [optionsError, setOptionsError] = useState<Error>();
  const [optionsRevision, setOptionsRevision] = useState(0);
  const [selected, setSelected] = useState<AnalyticEvent | null>(null);
  const pageSizeId = useId();
  const trigger = useRef<HTMLElement | null>(null);
  const refreshButton = useRef<HTMLButtonElement>(null);
  const current = result?.query === query ? result : null;
  const page = current?.page;
  const error = current?.error;
  const denied = error instanceof ApiError && error.status === 403;
  const { filters } = query;
  const hasFilters = !!(filters.period || filters.type.length || filters.site.length || filters.camera.length);
  const events = page?.items ?? [];
  const offset = query.pageIndex * query.pageSize;

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    function load() {void api<EventsPage>(`${base}?${pageParams(query)}`, "GET", undefined, true, controller.signal)
      .then(page => { if (!controller.signal.aborted) setResult({ query, page }); })
      .catch(error => { if (!controller.signal.aborted) setResult({ query, error }); })
      .finally(() => {if (!controller.signal.aborted && !query.pageIndex && !query.window && !query.filters.period) timer=setTimeout(load,10000);});}
    load();
    return () => {controller.abort();clearTimeout(timer);};
  }, [base, query]);

  useEffect(() => {
    const controller = new AbortController();
    void api<FilterOptions>(`${base}/filters`, "GET", undefined, true, controller.signal)
      .then(options => { if (!controller.signal.aborted) { setOptions(options); setOptionsError(undefined); } })
      .catch(error => { if (!controller.signal.aborted) { setOptions(filterOptions); setOptionsError(error); } });
    return () => controller.abort();
  }, [base, optionsRevision]);

  useEffect(() => onEnvironmentChange([], notification => {
    if (notification.tenantId !== tenantId) return;
    setSelected(null);
    setOptions(filterOptions);
    setQuery(query => ({ filters: query.filters, pageSize: query.pageSize, pageIndex: 0, cursors: [""] }));
    setOptionsRevision(revision => revision + 1);
  }), [tenantId]);

  function restart(filters: Filters, pageSize = query.pageSize) {
    setQuery({ filters, pageIndex: 0, pageSize, cursors: [""] });
  }
  function refresh() {
    restart(filters);
    setOptionsRevision(revision => revision + 1);
  }
  function navigatePage(index: number) {
    if (!page || index < 0) return;
    const forward = index > query.pageIndex;
    if (forward && !page.nextCursor) return;
    const cursors = forward ? [...query.cursors.slice(0, index), page.nextCursor] : query.cursors;
    setQuery({ ...query, pageIndex: index, cursors, window: { from: page.from, until: page.until } });
  }
  return <section className="analytic-events" aria-label="Eventos analíticos">
    <div className="page-heading">
      <h1>Eventos</h1>
      {!denied && <Button ref={refreshButton} variant="outline" size="sm" onClick={refresh} disabled={!current}><RefreshCw aria-hidden="true" />Atualizar</Button>}
    </div>
    {denied ? <Alert><AlertDescription>Sua conta precisa da permissão Visualizar eventos neste ambiente.</AlertDescription></Alert> : <>
      {(error || optionsError) && <Alert className="mb-3" variant="destructive" role="alert"><AlertDescription>{error?.message ?? "Não foi possível carregar as opções de filtro. Atualize a consulta para tentar novamente."}</AlertDescription></Alert>}
      <EventFailures key={base} base={base}/>
      <div className="analytic-events-filters" role="group" aria-label="Filtros de eventos">
        <PeriodFilter selected={filters.period} visiblePeriod={page ?? query.window} onChange={period => restart({ ...filters, period })} />
        <MultiFilter label="Tipo" options={typeOptions} selected={filters.type} onChange={type => restart({ ...filters, type })} />
        <MultiFilter label="Site" options={options.sites} selected={filters.site} onChange={site => restart({ ...filters, site })} />
        <MultiFilter label="Câmera" options={options.cameras.map(camera => ({ id: camera.id, name: `${camera.name} #${camera.id}` }))} selected={filters.camera} onChange={camera => restart({ ...filters, camera })} />
        {hasFilters && <Button variant="ghost" size="sm" onClick={() => restart(emptyFilters)}>Limpar filtros<X aria-hidden="true" /></Button>}
      </div>
      <ScrollArea className="analytic-events-history" role="region" aria-label="Histórico de eventos" aria-busy={!current}>
        {!current && <div className="grid gap-2 p-0.5" aria-hidden="true">{Array.from({ length: 4 }, (_, index) => <Skeleton key={index} className="h-[92px] rounded-xl" />)}</div>}
        {events.length > 0 && <ul className="grid list-none gap-2 p-0.5" role="list">
          {events.map(event => <li key={event.id}>
            <Card size="sm" className="p-0 shadow-none">
              <Button type="button" variant="ghost" className="analytic-event-card h-auto w-full whitespace-normal text-sm font-normal" aria-haspopup="dialog" onClick={click => { trigger.current = click.currentTarget; setSelected(event); }}>
                <span className="flex min-w-0 items-center gap-3">
                  <span className="size-20 shrink-0 rounded-md border bg-muted" data-event-thumbnail aria-hidden="true" />
                  <span className="flex min-w-0 flex-col gap-1">
                    <span className="flex flex-wrap items-center gap-2 font-semibold">{typeNames[event.type]}{event.state === "inactive" && <Badge variant="secondary">Encerramento</Badge>}{event.simulated && <Badge variant="outline">Simulado</Badge>}</span>
                    <span className="analytic-event-source text-xs">
                      <span>{event.siteName}</span><span>{event.cameraName} #{event.cameraId}</span>
                    </span>
                  </span>
                </span>
                <span className="analytic-event-end text-muted-foreground">
                  <time dateTime={event.receivedAt} className="text-xs tabular-nums">{dateFormat.format(new Date(event.receivedAt))}</time>
                  <ChevronRight className="size-4" aria-hidden="true" />
                </span>
              </Button>
            </Card>
          </li>)}
        </ul>}
        {page && !events.length && <Empty className="absolute inset-0"><EmptyHeader>
          <EmptyTitle>{hasFilters ? "Nenhum evento para os filtros aplicados" : "Nenhum evento nas últimas 24 horas"}</EmptyTitle>
          <EmptyDescription>{hasFilters ? "Ajuste os filtros para ampliar a consulta." : "Altere o período para consultar eventos anteriores."}</EmptyDescription>
        </EmptyHeader></Empty>}
      </ScrollArea>
      <div className="analytic-events-footer">
        <span role="status" className="text-muted-foreground">{!current ? "Carregando eventos…" : events.length ? `${offset + 1}–${offset + events.length} eventos` : "0 eventos"}</span>
        <div className="flex items-center gap-2">
          <Label htmlFor={pageSizeId}>Eventos por página</Label>
          <NativeSelect id={pageSizeId} value={query.pageSize} onChange={event => restart(filters, Number(event.target.value))}>
            {[10, 20, 30, 50, 100].map(size => <NativeSelectOption key={size} value={size}>{size}</NativeSelectOption>)}
          </NativeSelect>
        </div>
        <nav className="flex items-center gap-2" aria-label="Paginação de eventos">
          <span className="tabular-nums">Página {query.pageIndex + 1}</span>
          <Button type="button" variant="outline" size="icon-sm" aria-label="Página anterior" disabled={!page || query.pageIndex === 0} onClick={() => navigatePage(query.pageIndex - 1)}><ChevronLeft aria-hidden="true" /></Button>
          <Button type="button" variant="outline" size="icon-sm" aria-label="Próxima página" disabled={!page?.nextCursor} onClick={() => navigatePage(query.pageIndex + 1)}><ChevronRight aria-hidden="true" /></Button>
        </nav>
      </div>
    </>}
    {selected && !denied && <EventDetails event={selected} onClose={() => setSelected(null)} returnFocus={trigger.current} fallbackFocus={() => refreshButton.current} />}
  </section>;
}
