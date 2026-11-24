"use client";

import { useEffect, useState, type FormEvent } from "react";
import { FormDialog } from "@/components/form-dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";

const kinds = { intrusion: "Intrusão", line_crossing: "Cruzamento de linha" };
type Kind = keyof typeof kinds;
type Rule = { kind: Kind; id: string; label: string; schedule?: string };
type Settings = { enabled: boolean; revision: number; rules: Rule[] };
type Inventory = Partial<Record<Kind, { enabled: boolean; items: {id: string; enabled: boolean}[] | null; complete: boolean }>>;
type Verification = {inventory:Inventory; device:{model?:string;firmware?:string}; qualification: {types:Kind[];supervision:string;reconnect:string} | null};
const same = (a: Rule, b: Rule) => a.kind === b.kind && a.id === b.id;

export function EventCollectionDialog({base, name, trigger, onClose}: {base: string; name: string; trigger: HTMLElement; onClose: () => void}) {
  const [settings, setSettings] = useState<Settings>();
  const [initial, setInitial] = useState<Settings>();
  const [inventory, setInventory] = useState<Inventory>();
  const [verification, setVerification] = useState<Verification>();
  const [reading, setReading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const ctrl = new AbortController();
    api<Settings>(base, "GET", undefined, true, ctrl.signal).then(value => {
      if (!ctrl.signal.aborted) {setSettings(value); setInitial(value);}
    }).catch(() => {if (!ctrl.signal.aborted) setError("Não foi possível carregar a configuração.");});
    return () => ctrl.abort();
  }, [base]);
  async function read() {
    setReading(true); setError("");
    try {const value = await api<Verification>(base + "/rules", "GET", undefined, true);setInventory(value.inventory);setVerification(value);}
    catch {setInventory(undefined);setVerification(undefined); setError("Verificação indisponível. As regras já selecionadas continuam salvas.");}
    finally {setReading(false);}
  }
  async function save(e: FormEvent) {
    e.preventDefault(); if (!settings) return; setBusy(true); setError("");
    try {await api(base, "PUT", settings, true); onClose();}
    catch (err) {setError(err instanceof Error ? err.message : "Falha ao salvar a configuração.");}
    finally {setBusy(false);}
  }
  const options = [...(settings?.rules ?? [])];
  for (const kind of Object.keys(kinds) as Kind[]) {
    const channel = inventory?.[kind];
    if (!channel) continue;
    for (const id of ["", ...(channel.items ?? []).map(item => item.id)]) {
      const rule = {kind, id, label: id ? kinds[kind] + " #" + id : kinds[kind] + " (canal)"};
      if (!options.some(item => same(rule, item))) options.push(rule);
    }
  }
  return <FormDialog title={"Recepção de eventos · " + name} returnFocus={trigger} onClose={onClose}
    onSubmit={save} busy={busy} hasChanges={JSON.stringify(initial) !== JSON.stringify(settings)} saveDisabled={!settings}>
    {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
    {settings ? <div className="grid gap-4">
      <Label><Checkbox checked={settings.enabled} onCheckedChange={v => setSettings({...settings, enabled: v === true})}/>Habilitar coleta de eventos</Label>
      {verification && <div className="grid gap-1 text-sm"><span><Badge variant="secondary">{verification.qualification ? "Homologada" : "Em validação"}</Badge> {verification.device.model} {verification.device.firmware}</span>
        {verification.qualification && <p>{verification.qualification.types.map(k => kinds[k]).join(", ")} · {verification.qualification.supervision === "rule" ? "Supervisão por regra" : "Supervisão por canal"} · {verification.qualification.reconnect}</p>}
      </div>}
      <section className="grid gap-3"><h3>Regras supervisionadas</h3>
        <p className="text-sm text-muted-foreground">Selecione as regras que devem permanecer habilitadas. O histórico recebe alertas de todas as linhas e regiões.</p>
        <Button type="button" variant="outline" size="sm" disabled={reading} onClick={read}>{reading ? "Consultando…" : "Consultar regras da câmera"}</Button>
        {inventory && (Object.keys(kinds) as Kind[]).map(kind => <p key={kind} className="text-sm">{kinds[kind]} · {inventory[kind] ? (inventory[kind]?.enabled ? "Canal habilitado" : "Canal desabilitado") : "Verificação indisponível"}{inventory[kind] && !inventory[kind]?.complete && " · Verificação individual indisponível"}</p>)}
        {options.map(option => {
          const chosen = settings.rules.find(r => same(r, option));
          const channel = inventory?.[option.kind];
          const item = channel?.items?.find(rule => rule.id === option.id);
          const identity = kinds[option.kind] + (option.id ? " #" + option.id : " (canal)");
          const observed = !option.id || !channel ? "" : item ? (item.enabled ? "Habilitada" : "Desabilitada") : channel.complete ? "Não encontrada" : "Verificação individual indisponível";
          return <div className="grid gap-2" key={option.kind + "/" + option.id}>
            <Label><Checkbox checked={!!chosen} onCheckedChange={v => setSettings({...settings, rules: v === true ? [...settings.rules, option] : settings.rules.filter(r => !same(r, option))})}/>{identity}{observed && <span className="text-muted-foreground"> · {observed}</span>}</Label>
            {chosen && chosen.id && <Label className="grid gap-1">Nome de referência<Input maxLength={512} value={chosen.label} onChange={e => setSettings({...settings, rules: settings.rules.map(r => same(r, chosen) ? {...r, label: e.target.value} : r)})}/></Label>}
            {chosen && <Label className="grid gap-1">Horário de referência<Input maxLength={512} value={chosen.schedule ?? ""} placeholder="Ex.: 24 horas" onChange={e => setSettings({...settings, rules: settings.rules.map(r => same(r, chosen) ? {...r, schedule: e.target.value} : r)})}/></Label>}
          </div>;
        })}
      </section>
    </div> : !error && <p role="status">Carregando configuração…</p>}
  </FormDialog>;
}

type Failure = {id: string; code: string; condition: string; camera: string; affected?: string[]; openedAt: string; closedAt?: string; endReason?: string};
const failureNames: Record<string,string> = {
  coverage_uncertain: "Cobertura incerta", verification_unavailable: "Verificação indisponível", rule_missing: "Regra esperada não encontrada",
  rule_disabled: "Regra desabilitada", channel_only: "Analítico habilitado no canal · verificação individual indisponível", channel_disabled: "Analítico desabilitado no canal", queue_full: "Fila do tenant saturada",
  collector_unreachable: "Comunicação com o coletor indisponível", collection_after_deletion: "Coleta após a exclusão"
};
const ends: Record<string,string> = {restored:"Restabelecida", suspended:"Supervisão suspensa pelo operador", changed:"Condição alterada", configuration_changed:"Configuração alterada",deleted:"Câmera excluída"};
const dates = new Intl.DateTimeFormat("pt-BR", {dateStyle:"short",timeStyle:"medium"});
export function EventFailures({base}: {base: string}) {
  const [closed, setClosed] = useState(false);
  const [data, setData] = useState<{items: Failure[];nextCursor?: string}>();
  const [cursors, setCursors] = useState<string[]>([""]);
  const cursor = cursors[cursors.length - 1];
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const ctrl = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    setData(undefined);setFailed(false);
    async function load() {
      try {
        const value = await api<{items:Failure[];nextCursor:string}>(base + "/failures?closed=" + (closed ? "1" : "0") + "&cursor=" + encodeURIComponent(cursor), "GET", undefined, true, ctrl.signal);
        if (!ctrl.signal.aborted) {setData(value);setFailed(false);}
      } catch {if (!ctrl.signal.aborted) {setData(undefined);setFailed(true);}}
      if (!ctrl.signal.aborted) timer = setTimeout(load, 10000);
    }
    void load();
    return () => {ctrl.abort();clearTimeout(timer);};
  }, [base,closed,cursor]);
  return <section aria-label="Supervisão da coleta" className="mb-3 rounded-lg border px-3 py-2">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-sm font-semibold">{closed ? "Falhas encerradas" : "Falhas abertas"}{data && cursors.length === 1 ? " (" + data.items.length + (data.nextCursor ? "+" : "") + ")" : ""}</h2>
      <Button type="button" variant="ghost" size="sm" onClick={() => {setData(undefined);setCursors([""]);setClosed(!closed);}}>{closed ? "Ver abertas" : "Ver encerradas"}</Button>
    </div>
    {failed && <p role="status">Não foi possível consultar a supervisão.</p>}
    {data && data.items.length > 0 && <ul className="max-h-44 overflow-auto text-sm" aria-label="Falhas de supervisão">
      {data.items.map(f => {
        const [,kind,id] = f.condition.split("/");
        return <li key={f.id} className="border-t py-2"><strong>{failureNames[f.code] ?? "Falha de supervisão"}</strong>
          {f.camera && " · Câmera #" + f.camera}{kind && " · " + (kinds[kind as Kind] ?? kind)}{id && " #" + id}
          {f.affected?.length ? " · Câmeras " + f.affected.map(c => "#" + c).join(", ") : ""}
          <div className="text-xs text-muted-foreground">Desde {dates.format(new Date(f.openedAt))}{f.closedAt && " · " + (ends[f.endReason ?? ""] ?? "Encerrada") + " em " + dates.format(new Date(f.closedAt))}</div></li>;
      })}
    </ul>}
    {(cursors.length > 1 || data?.nextCursor) && <nav aria-label="Páginas de falhas" className="flex gap-2">
      <Button type="button" variant="outline" size="sm" disabled={cursors.length === 1} onClick={() => setCursors(cursors.slice(0,-1))}>Anterior</Button>
      <Button type="button" variant="outline" size="sm" disabled={!data?.nextCursor} onClick={() => {if(data?.nextCursor)setCursors([...cursors,data.nextCursor]);}}>Próxima</Button>
    </nav>}
  </section>;
}
