import Workspace from "@/components/workspace";
import { workspacePaths } from "@/lib/workspace-routes";

export const dynamicParams = false;

export function generateStaticParams() {
  return Object.values(workspacePaths).map(path => ({ section: path.slice(1) }));
}

export default function Page() {
  return <Workspace />;
}
