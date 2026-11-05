"""Prepare the MIT source release with vendor integrations and without private manuals/React Bits."""

import argparse
import hashlib
import importlib.util
import json
import re
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("source_selection", ROOT / "tools/prepare-source.py")
selection = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selection)

EXCLUDED = {
    "frontend/src/components/lightfall.tsx",
    "frontend/src/components/lightfall.css",
    "frontend/src/components/gradient-ascii.tsx",
    "frontend/src/components/gradient-ascii.css",
    "frontend/docs/licenses/react-bits.md",
    "backend/docs/hikvision.md",
}


def text(raw):
    return raw.decode("utf-8-sig")


def prepare():
    files = {name: raw for name, raw in selection.selected_files()
             if name not in EXCLUDED and not name.startswith("frontend/tests/login-background/")}
    workspace = "frontend/src/components/workspace.tsx"
    content = text(files[workspace])
    if 'import Lightfall from "@/components/lightfall";' in content:
        content = content.replace('import Lightfall from "@/components/lightfall";',
                                  'import OrbitalDawn from "@/components/orbital-dawn";')
        content, count = re.subn(r"<Lightfall\b[^>]*\/>", "<OrbitalDawn/>", content)
        if count != 1:
            raise ValueError("Unexpected login markup; review the replacement")
    elif '<OrbitalDawn' not in content:
        raise ValueError("Unknown login background; review before exporting")
    files[workspace] = content.encode("utf-8")
    if any("@/components/lightfall" in text(raw) or "@/components/gradient-ascii" in text(raw)
           for name, raw in files.items() if name.endswith((".ts", ".tsx"))):
        raise ValueError("A remaining source imports an excluded component")

    readme = text(files["README.md"])
    readme = re.sub(r"\*\*Estado:.*?(?=\n\n## Recursos)",
                    "**Estado: em desenvolvimento.** Código próprio disponível sob a [licença MIT](LICENSE). Veja [publicação e privacidade](docs/publication.md). A instalação em produção ainda requer homologação.", readme, flags=re.S)
    readme = re.sub(r"O código também contém uma integração.*?(?=\n\n## Arquitetura)",
                    "As integrações Dahua e Hikvision são mantidas, incluindo os recursos PTZ já implementados. Manuais de fabricantes e suas extrações não são distribuídos com o código.", readme, flags=re.S)
    readme = re.sub(r"## Licença e terceiros.*\Z",
                    "## Licença e terceiros\n\nLicença MIT para o código próprio: consulte [LICENSE](LICENSE).\n\nAs dependências conservam suas próprias licenças. Esta variante usa o fundo CSS original OrbitalDawn e não distribui os componentes React Bits. Consulte [THIRD-PARTY.md](backend/deploy/windows/THIRD-PARTY.md) antes de distribuir binários.\n", readme, flags=re.S)
    files["README.md"] = readme.encode("utf-8")
    files["LICENSE"] = files["tools/github/LICENSE-MIT.txt"]
    files["docs/publication.md"] = files["tools/github/publication.md"]
    contribution = text(files["CONTRIBUTING.md"])
    contribution = re.sub(r"A publicação e a escolha da licença.*?(?=\n\n)",
                          "O código próprio é distribuído sob a licença MIT, apresentada em LICENSE. Dependências de terceiros conservam seus próprios termos.", contribution, count=1, flags=re.S)
    files["CONTRIBUTING.md"] = contribution.encode("utf-8")
    for name in ("backend/README.md", "frontend/README.md"):
        content = text(files[name]).replace("A licença do código próprio está pendente;", "A licença do código próprio é MIT;")
        files[name] = content.encode("utf-8")
    # Keep the Astra PTZ contract; omit the manufacturer protocol/manual section.
    ptz = "backend/docs/ptz.md"
    if ptz in files:
        content = re.sub(r"## Adaptador Hikvision.*?(?=## Validação e limites)", "", text(files[ptz]), flags=re.S)
        files[ptz] = content.encode("utf-8")
    files["frontend/docs/ui.md"] = (
        "# Interface do Astra\n\nA interface usa shadcn/ui, tema escuro, cores ciano e fonte Inter. "
        "Os componentes compartilhados ficam em src/components/ui; os estilos gerais em src/app.\n\n"
        "A variante pública usa a cena CSS original OrbitalDawn no login. Os componentes derivados "
        "de React Bits e os respectivos ensaios WebGL não são distribuídos nesta variante.\n\n"
        "A marca e os ícones usam os ativos Astra presentes em public/brand e src/app. "
        "A interface consulta a API na mesma origem HTTPS e respeita as permissões por tenant.\n"
    ).encode("utf-8")
    for path in ("frontend/package.json", "frontend/package-lock.json"):
        package = json.loads(text(files[path]))
        package["license"] = "MIT"
        if path.endswith("package-lock.json"):
            package["packages"][""]["license"] = "MIT"
        files[path] = (json.dumps(package, indent=2, ensure_ascii=False) + "\n").encode("utf-8")
    # Keep the decision text but do not leave links to private local tickets.
    for name, raw in list(files.items()):
        if name.endswith(".md"):
            content = re.sub(r"\[([^\]]+)\]\([^)]*\.scratch/[^)]*\)",
                             r"\1 (especificação local não distribuída)", text(raw))
            files[name] = content.encode("utf-8")
    # The exported list describes the exported files, so both preparation tools
    # can be rerun against the review copy without missing excluded files.
    files["tools/public-source-files.txt"] = (
        "# Selected public sources; review contents when changing this list.\n" +
        "\n".join(sorted(files)) + "\n"
    ).encode("utf-8")
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", required=True, type=Path)
    args = parser.parse_args()
    files = prepare()
    destination = args.destination.absolute()
    for component in [destination, *destination.parents]:
        if component.is_symlink() or component.is_junction():
            raise ValueError("Destination must not traverse links/junctions")
    if destination.exists():
        raise ValueError("Destination must be new")
    destination.mkdir(parents=True)
    manifest = []
    for name, raw in sorted(files.items()):
        target = destination.joinpath(*PurePosixPath(name).parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(raw)
        manifest.append({"path": name, "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()})
    (destination / "source-manifest.json").write_text(json.dumps({
        "purpose": "MIT source release approved by maintainer",
        "vendor_integrations": "preserved",
        "private_manuals": "excluded",
        "react_bits_components": "excluded; original OrbitalDawn used",
        "files": manifest,
    }, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {len(files)} files: {destination}")
    print("MIT release; vendor integrations preserved; private manuals excluded.")


if __name__ == "__main__":
    main()
