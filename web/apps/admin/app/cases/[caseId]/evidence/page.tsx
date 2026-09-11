import { CaseEvidence } from "./CaseEvidence";

type PageProps = {
  params: Promise<{ caseId: string }>;
};

export default async function CaseEvidencePage({ params }: PageProps) {
  const { caseId } = await params;
  return <CaseEvidence caseId={caseId} />;
}
