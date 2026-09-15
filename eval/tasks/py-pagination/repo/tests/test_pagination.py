import unittest

from pagination import page_links, paginate


class PaginateTest(unittest.TestCase):
    def setUp(self):
        self.orders = [f"order-{i}" for i in range(45)]

    def test_first_page(self):
        page = paginate(self.orders, page=1, per_page=20)
        self.assertEqual(page.items, self.orders[:20])
        self.assertEqual(page.total, 45)
        self.assertEqual(page.total_pages, 3)
        self.assertFalse(page.has_prev)
        self.assertTrue(page.has_next)

    def test_middle_page(self):
        page = paginate(self.orders, page=2, per_page=20)
        self.assertEqual(page.items, self.orders[20:40])
        self.assertTrue(page.has_prev)
        self.assertTrue(page.has_next)

    def test_last_partial_page(self):
        page = paginate(self.orders, page=3, per_page=20)
        self.assertEqual(page.items, self.orders[40:])
        self.assertTrue(page.has_prev)
        self.assertFalse(page.has_next)

    def test_exact_multiple(self):
        items = list(range(40))
        self.assertEqual(paginate(items, page=1, per_page=20).total_pages, 2)
        last = paginate(items, page=2, per_page=20)
        self.assertEqual(last.items, list(range(20, 40)))
        self.assertFalse(last.has_next)

    def test_empty_list_has_one_page(self):
        page = paginate([], page=1, per_page=10)
        self.assertEqual(page.items, [])
        self.assertEqual(page.total_pages, 1)
        self.assertFalse(page.has_prev)
        self.assertFalse(page.has_next)

    def test_page_after_last_is_empty(self):
        page = paginate(self.orders, page=5, per_page=20)
        self.assertEqual(page.items, [])
        self.assertTrue(page.has_prev)
        self.assertFalse(page.has_next)

    def test_per_page_one(self):
        page = paginate(["a", "b", "c"], page=3, per_page=1)
        self.assertEqual(page.items, ["c"])
        self.assertEqual(page.total_pages, 3)
        self.assertFalse(page.has_next)

    def test_accepts_any_sequence(self):
        self.assertEqual(paginate(range(10), page=2, per_page=4).items, [4, 5, 6, 7])
        self.assertEqual(paginate(tuple("abcde"), page=1, per_page=2).items, ["a", "b"])

    def test_invalid_arguments(self):
        for kwargs in ({"page": 0}, {"page": -1}, {"per_page": 0}, {"per_page": 101}):
            with self.subTest(**kwargs), self.assertRaises(ValueError):
                paginate(self.orders, **kwargs)


class PageLinksTest(unittest.TestCase):
    def test_gaps_on_both_sides(self):
        self.assertEqual(page_links(6, 12), [1, None, 4, 5, 6, 7, 8, None, 12])

    def test_near_start(self):
        self.assertEqual(page_links(1, 12), [1, 2, 3, None, 12])
        self.assertEqual(page_links(4, 12), [1, 2, 3, 4, 5, 6, None, 12])

    def test_gap_of_one_page_shows_the_page(self):
        self.assertEqual(page_links(5, 12), [1, 2, 3, 4, 5, 6, 7, None, 12])
        self.assertEqual(page_links(8, 12), [1, None, 6, 7, 8, 9, 10, 11, 12])

    def test_near_end(self):
        self.assertEqual(page_links(12, 12), [1, None, 10, 11, 12])

    def test_small_page_counts(self):
        self.assertEqual(page_links(1, 1), [1])
        self.assertEqual(page_links(2, 3), [1, 2, 3])
        self.assertEqual(page_links(1, 0), [])

    def test_custom_window(self):
        self.assertEqual(page_links(10, 20, window=1), [1, None, 9, 10, 11, None, 20])


if __name__ == "__main__":
    unittest.main()
