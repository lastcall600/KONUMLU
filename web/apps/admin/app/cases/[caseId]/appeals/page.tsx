import { CaseAppeals } from "./CaseAppeals";

type PageProps = {
  params: Promise<{ caseId: string }>;
};

export default async function CaseAppealsPage({ params }: PageProps) {
  const { caseId } = await params;
  return <CaseAppeals caseId={caseId} />;
}
