import unittest

from .support import run_cli, temp_repo, write_prd


class DiscoverPrdsTests(unittest.TestCase):
    def test_omitting_the_prd_path_lists_available_prds_in_sorted_order(self):
        with temp_repo() as repo:
            write_prd(repo, "zeta-feature")
            write_prd(repo, "alpha-feature")

            exit_code, stdout, stderr = run_cli([])

            self.assertEqual(exit_code, 0, stderr)
            alpha_index = stdout.index(".scratch/alpha-feature/PRD.md")
            zeta_index = stdout.index(".scratch/zeta-feature/PRD.md")
            self.assertLess(alpha_index, zeta_index)

    def test_omitting_the_prd_path_with_no_prds_reports_none_found(self):
        with temp_repo():
            exit_code, stdout, stderr = run_cli([])

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn("No PRDs found", stdout)


if __name__ == "__main__":
    unittest.main()
