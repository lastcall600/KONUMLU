import { CaseActions } from "./CaseActions";

type PageProps = {
  params: Promise<{ caseId: string }>;
};

export default async function CaseActionsPage({ params }: PageProps) {
  const { caseId } = await params;
  return <CaseActions caseId={caseId} />;
}
