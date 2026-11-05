"use client";

import { useState, type FormEvent } from "react";
import { AtSign, Check, Eye, EyeOff, KeyRound, LockKeyhole, Mail, Save, ShieldCheck, UserRound } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Spinner } from "@/components/ui/spinner";
import { api, ApiError, type Operator } from "@/lib/api";
import "./account.css";

function PasswordField({ id, name, label, current = false }: { id: string; name: string; label: string; current?: boolean }) {
  const [visible, setVisible] = useState(false);
  return <div className="account-field">
    <Label htmlFor={id}>{label}</Label>
    <div className="account-input-wrap">
      <LockKeyhole aria-hidden="true" className="account-input-icon" />
      <Input id={id} name={name} type={visible ? "text" : "password"} autoComplete={current ? "current-password" : "new-password"} maxLength={1024} required className="account-password-input" />
      <Button type="button" variant="ghost" size="icon" className="account-reveal" onClick={() => setVisible(!visible)} aria-label={`${visible ? "Ocultar" : "Mostrar"} ${label.toLowerCase()}`} aria-pressed={visible}>
        {visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
      </Button>
    </div>
  </div>;
}

export function Account({ user, onUpdate }: { user: Operator; onUpdate: (user: Operator) => void }) {
  const [name, setName] = useState(user.name || user.username);
  const [email, setEmail] = useState(user.email || "");
  const [profilePassword, setProfilePassword] = useState("");
  const [profileError, setProfileError] = useState("");
  const [profileMessage, setProfileMessage] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [passwordMessage, setPasswordMessage] = useState("");
  const [busy, setBusy] = useState<"profile" | "password" | null>(null);
  const displayName = user.name || user.username;
  const initials = displayName.trim().split(/\s+/).slice(0, 2).map(part => Array.from(part)[0]).join("").toUpperCase();
  const emailChanged = email.trim().toLowerCase() !== (user.email || "").toLowerCase();
  const dirty = name.trim() !== (user.name || user.username) || email.trim() !== (user.email || "");

  function resetProfile() {
    setName(user.name || user.username); setEmail(user.email || ""); setProfilePassword("");
    setProfileError(""); setProfileMessage("");
  }

  async function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("profile"); setProfileError(""); setProfileMessage("");
    try {
      const updated = await api<Operator>("/me", "PATCH", { name, email, revision: user.revision, currentPassword: profilePassword });
      onUpdate(updated); setName(updated.name); setEmail(updated.email); setProfilePassword("");
      setProfileMessage("Pronto! Seus dados foram atualizados.");
    } catch (error) { setProfileError((error as Error).message); }
    finally { setBusy(null); }
  }

  async function savePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget, values = new FormData(form);
    setPasswordError(""); setPasswordMessage("");
    if (values.get("new") !== values.get("confirm")) {
      setPasswordError("A confirmação deve ser igual à nova senha."); return;
    }
    setBusy("password");
    try {
      await api("/auth/password", "POST", { currentPassword: values.get("current"), newPassword: values.get("new") });
      form.reset();
      setPasswordMessage("Senha alterada. Suas outras sessões foram encerradas.");
      try {
        const identity = await api<{ user: Operator }>("/me"); onUpdate(identity.user);
      } catch { setPasswordError("Sua senha foi salva. Atualize a página antes de editar seus dados pessoais."); }
    } catch (error) {
      setPasswordError(error instanceof ApiError && error.code === "invalid_credentials" ? "A senha atual está incorreta." : (error as Error).message);
    } finally { setBusy(null); }
  }

  return <div className="account-page">
    <div className="page-heading account-heading"><div><span className="eyebrow">SEU ESPAÇO NO ASTRA</span><h1>Minha conta</h1><p>Seus dados e suas opções de acesso.</p></div></div>
    <section className="account-summary" aria-label="Resumo do perfil">
      <div className="account-avatar" aria-hidden="true">{initials}<span><UserRound /></span></div>
      <div className="account-summary-identity"><span className="account-kicker">PERFIL DO OPERADOR</span><h2>{displayName}</h2><span className="account-summary-email"><Mail aria-hidden="true" />{user.email || "Adicione seu e-mail para entrar com ele também"}</span></div>
      <div className="account-summary-role"><ShieldCheck aria-hidden="true" /><div><strong>{user.globalAdmin ? "Administrador da instalação" : "Operador"}</strong><span><i />{user.active ? "Conta ativa" : "Conta inativa"}</span></div></div>
    </section>

    <Tabs defaultValue="profile" className="account-tabs">
      <TabsList variant="line" className="account-tab-list" aria-label="Configurações da conta">
        <TabsTrigger value="profile" disabled={busy !== null}><UserRound aria-hidden="true" />Dados pessoais</TabsTrigger>
        <TabsTrigger value="security" disabled={busy !== null}><ShieldCheck aria-hidden="true" />Segurança</TabsTrigger>
      </TabsList>
      <TabsContent value="profile">
        <section className="account-panel">
          <div className="account-section-intro"><span className="account-section-icon"><UserRound aria-hidden="true" /></span><h2>Um perfil com a sua identidade</h2><p>Mantenha seu nome e e-mail atualizados em todos os tenants que você acessa.</p><div className="account-tip"><AtSign aria-hidden="true" /><span>Entre no Astra usando seu usuário ou o e-mail cadastrado.</span></div></div>
          <form onSubmit={saveProfile} aria-label="Dados pessoais" aria-busy={busy === "profile"}>
            <input type="hidden" name="username" autoComplete="username" value={user.username} />
            {profileError && <Alert role="alert" variant="destructive" className="account-feedback"><AlertDescription>{profileError}</AlertDescription></Alert>}
            {profileMessage && <Alert role="status" className="success account-feedback"><Check aria-hidden="true" /><AlertDescription>{profileMessage}</AlertDescription></Alert>}
            <fieldset disabled={busy !== null}>
              <div className="account-field"><Label htmlFor="account-name">Nome completo</Label><div className="account-input-wrap"><UserRound aria-hidden="true" className="account-input-icon" /><Input id="account-name" name="name" autoComplete="name" value={name} onChange={e => { setName(e.target.value); setProfileMessage(""); }} maxLength={128} required /></div></div>
              <div className="account-field"><Label htmlFor="account-email">Endereço de e-mail</Label><div className="account-input-wrap"><Mail aria-hidden="true" className="account-input-icon" /><Input id="account-email" name="email" type="email" autoComplete="email" autoCapitalize="none" spellCheck={false} value={email} onChange={e => { setEmail(e.target.value); setProfileMessage(""); }} maxLength={254} required aria-describedby="account-email-hint" /></div><p id="account-email-hint" className="account-field-hint">Você também pode usar este endereço para entrar.</p></div>
              {emailChanged && <div className="account-email-confirm"><Label htmlFor="account-email-password">Confirme sua senha atual</Label><Input id="account-email-password" name="currentPassword" type="password" autoComplete="current-password" value={profilePassword} onChange={e => setProfilePassword(e.target.value)} maxLength={1024} required aria-describedby="account-email-password-hint" /><p id="account-email-password-hint" className="account-field-hint">Para proteger seu acesso, confirme a alteração do e-mail com sua senha.</p></div>}
              <div className="account-username"><span className="account-username-icon"><AtSign aria-hidden="true" /></span><div><span>Usuário de acesso</span><strong>{user.username}</strong></div><LockKeyhole aria-label="Usuário não editável" /></div>
              <div className="account-form-footer"><span>{dirty ? "Você tem alterações para salvar" : "Tudo atualizado por aqui"}</span><div><Button type="button" variant="outline" onClick={resetProfile} disabled={!dirty}>Cancelar</Button><Button type="submit" disabled={!dirty}>{busy === "profile" ? <Spinner aria-hidden="true" /> : <Save aria-hidden="true" />}{busy === "profile" ? "Salvando…" : "Salvar alterações"}</Button></div></div>
            </fieldset>
          </form>
        </section>
      </TabsContent>
      <TabsContent value="security">
        <section className="account-panel">
          <div className="account-section-intro"><span className="account-section-icon"><KeyRound aria-hidden="true" /></span><h2>Cuide do seu acesso</h2><p>Use uma senha exclusiva para sua conta e evite compartilhá-la.</p><div className="account-tip"><ShieldCheck aria-hidden="true" /><span>Ao alterar a senha, suas outras sessões são encerradas. Esta sessão continua ativa.</span></div></div>
          <form onSubmit={savePassword} aria-label="Alterar senha" aria-busy={busy === "password"}>
            <input type="hidden" name="username" autoComplete="username" value={user.username} />
            {passwordError && <Alert role="alert" variant="destructive" className="account-feedback"><AlertDescription>{passwordError}</AlertDescription></Alert>}
            {passwordMessage && <Alert role="status" className="success account-feedback"><Check aria-hidden="true" /><AlertDescription>{passwordMessage}</AlertDescription></Alert>}
            <fieldset disabled={busy !== null}>
              <PasswordField id="account-current-password" name="current" label="Senha atual" current />
              <div className="account-password-divider" />
              <PasswordField id="account-new-password" name="new" label="Nova senha" />
              <PasswordField id="account-confirm-password" name="confirm" label="Confirmar nova senha" />
              <div className="account-form-footer"><span><LockKeyhole aria-hidden="true" />Confirme sua senha atual para salvar</span><Button type="submit">{busy === "password" ? <Spinner aria-hidden="true" /> : <KeyRound aria-hidden="true" />}{busy === "password" ? "Salvando…" : "Atualizar senha"}</Button></div>
            </fieldset>
          </form>
        </section>
      </TabsContent>
    </Tabs>
    <p className="account-footnote"><ShieldCheck aria-hidden="true" />Sua conta é a mesma em todos os tenants autorizados.</p>
  </div>;
}
