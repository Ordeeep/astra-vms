"use client";

import {useEffect, useId, useState} from "react";
import {Building2, ChevronRight, Search} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Checkbox} from "@/components/ui/checkbox";
import {Input} from "@/components/ui/input";
import {Label} from "@/components/ui/label";
import {NativeSelect, NativeSelectOption} from "@/components/ui/native-select";
import {permissionActions, sameTarget, setTargetAction, targetActionState, type Grant} from "@/lib/permission-grants";
import "./permission-group-editor.css";

type SiteTarget = {scope: "tenant" | "site"; resourceId: string};
type Props = {
  tenantId: string;
  sites: {id: string; name: string}[];
  grants: Grant[];
  onChange: (grants: Grant[]) => void;
  onSummaryChange: (summary: string) => void;
  loading?: boolean;
  error?: string;
  onRetry: () => void;
};
const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

export function PermissionGroupSummary({text}: {text: string}) {
  return text ? <div className="permission-summary" role="status"><span>{text}</span></div> : null;
}

export function PermissionGroupEditor({tenantId, sites, grants, onChange, onSummaryChange, loading, error, onRetry}: Props) {
  const id = useId();
  const tenantTarget: SiteTarget = {scope: "tenant", resourceId: tenantId};
  const [selected, setSelected] = useState<SiteTarget>(tenantTarget);
  const [query, setQuery] = useState("");
  // Legacy camera grants are retained as-is, never promoted to site grants implicitly.
  const legacyCameraCount = grants.filter(grant => grant.scope === "camera").length;
  const unavailableSites = grants.filter(grant => grant.scope === "site" && !sites.some(site => site.id === grant.resourceId))
    .filter((grant, index, all) => all.findIndex(other => other.resourceId === grant.resourceId) === index)
    .map(grant => ({id: grant.resourceId, name: `Site indisponível (${grant.resourceId})`}));
  const allSites = [...sites, ...unavailableSites].sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));
  const filteredSites = allSites.filter(site => normalize(site.name).includes(normalize(query)));
  const title = selected.scope === "tenant" ? "Todo o tenant" : allSites.find(site => site.id === selected.resourceId)?.name ?? "Site indisponível";
  const stateFor = (action: string) => targetActionState(grants, selected, action, tenantId);
  const visibleActions = permissionActions.filter(action => !("tenantOnly" in action) || selected.scope === "tenant" || stateFor(action.id).direct);
  const actionCount = visibleActions.filter(action => stateFor(action.id).checked).length;
  const unknownActions = [...new Set(grants.filter(grant => sameTarget(grant, selected) && !permissionActions.some(action => action.id === grant.action)).map(grant => grant.action))];
  const summary = loading || error ? "" : `${title} · ${actionCount + unknownActions.length} ${(actionCount + unknownActions.length) === 1 ? "ação habilitada" : "ações habilitadas"}`;
  useEffect(() => onSummaryChange(summary), [summary, onSummaryChange]);

  function resourceButton(target: SiteTarget, name: string) {
    const count = new Set(grants.filter(grant => sameTarget(grant, target)).map(grant => grant.action)).size;
    return <button type="button" className="permission-resource" aria-pressed={sameTarget(selected, target)} onClick={() => setSelected(target)} title={name}>
      <Building2 aria-hidden="true"/><span className="permission-resource-name">{name}</span>
      {count > 0 && <span className="permission-resource-count" aria-label={`${count} ações concedidas aqui`}>{count}</span>}
    </button>;
  }
  function actionRow(action: typeof permissionActions[number]) {
    const state = stateFor(action.id);
    const tenantOnlyHere = "tenantOnly" in action && selected.scope !== "tenant";
    const description = tenantOnlyHere ? "Concessão antiga. Esta ação precisa ser configurada em Todo o tenant para funcionar." : action.description;
    return <div className="permission-action" key={action.id}>
      <label className="permission-action-label" title={description}>
        <Checkbox aria-label={action.label} aria-describedby={`${id}-${action.id}-description`} checked={state.checked}
          disabled={(!state.direct && state.parents.length > 0) || !!loading || !!error}
          onCheckedChange={checked => onChange(setTargetAction(grants, selected, action.id, checked === true))}/>
        <span>{action.label}</span><span className="sr-only" id={`${id}-${action.id}-description`}>{description}</span>
      </label>
      {state.parents.length > 0 && <button type="button" className="permission-origin" aria-label={`Editar origem de ${action.label}: Todo o tenant`}
        title={state.direct ? "Também concedido aqui. Remover a concessão local mantém o acesso concedido no tenant." : "Concedido para todo o tenant. Edite a origem para remover este acesso."}
        onClick={() => setSelected(tenantTarget)}>Todo o tenant<ChevronRight aria-hidden="true"/></button>}
    </div>;
  }

  return <div className="permission-editor">
    {loading ? <p role="status">Carregando sites…</p> : error ? <div role="alert" className="permission-catalog-error"><p>{error}</p><Button type="button" variant="outline" onClick={onRetry}>Tentar novamente</Button></div> : <>
      <Label className="permission-mobile-target" htmlFor={`${id}-target`}>Site
        <NativeSelect id={`${id}-target`} value={JSON.stringify(selected)} onChange={event => setSelected(JSON.parse(event.target.value) as SiteTarget)}>
          <NativeSelectOption value={JSON.stringify(tenantTarget)}>Todo o tenant</NativeSelectOption>
          {allSites.map(site => <NativeSelectOption key={site.id} value={JSON.stringify({scope: "site", resourceId: site.id})}>{site.name}</NativeSelectOption>)}
        </NativeSelect>
      </Label>
      <div className="permission-workspace">
        <nav className="permission-resources" aria-label="Sites do grupo">
          <div className="permission-search"><Search aria-hidden="true"/><Input type="search" aria-label="Buscar site" placeholder="Buscar site…" value={query} onChange={event => setQuery(event.target.value)}/></div>
          <div className="permission-resource-list">
            {resourceButton(tenantTarget, "Todo o tenant")}
            {filteredSites.map(site => <div key={site.id}>{resourceButton({scope: "site", resourceId: site.id}, site.name)}</div>)}
            {!filteredSites.length && <p className="permission-empty" role="status">{query ? "Nenhum site encontrado." : "Nenhum site cadastrado."}</p>}
          </div>
        </nav>
        <section className="permission-details" aria-label={`Permissões: ${title}`}>
          <div className="permission-target-heading"><h3>{title}</h3><p>{selected.scope === "tenant" ? "Inclui todos os sites e suas câmeras." : "Vale para todas as câmeras deste site."}</p></div>
          <section aria-label="Operação"><h4>Operação</h4>{visibleActions.filter(action => action.section === "operation").map(actionRow)}</section>
          <section className="permission-administration" aria-label="Administração"><h4>Administração</h4>{visibleActions.filter(action => action.section === "administration").map(actionRow)}</section>
          {unknownActions.length > 0 && <section className="permission-unknown"><h4>Outras concessões existentes</h4>{unknownActions.map(action => <label key={action} className="permission-action-label"><Checkbox aria-label={action} checked onCheckedChange={() => onChange(setTargetAction(grants, selected, action, false))}/><span>{action}</span></label>)}</section>}
          {stateFor("export").checked && !stateFor("playback").checked && <p className="permission-note">Para exportar, permita também consultar as gravações das mesmas câmeras.</p>}
        </section>
      </div>
      {legacyCameraCount > 0 && <p className="permission-legacy-note" role="note">Este grupo mantém {legacyCameraCount} {legacyCameraCount === 1 ? "concessão antiga" : "concessões antigas"} por câmera. Esses acessos continuam ativos e serão preservados ao salvar.</p>}
    </>}
    <PermissionGroupSummary text={summary}/>
  </div>;
}
